// Command back-orbit runs the Back-Orbit server: the REST API, Server-Sent
// Events, and (in production builds) the embedded frontend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/back-orbit/back-orbit/internal/api"
	"github.com/back-orbit/back-orbit/internal/config"
	"github.com/back-orbit/back-orbit/internal/database"
	"github.com/back-orbit/back-orbit/internal/docker"
	"github.com/back-orbit/back-orbit/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("back-orbit exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()

	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		// A Docker client we can't even construct (e.g. a malformed host
		// address) is a configuration error, but it shouldn't take down the
		// rest of Back-Orbit — Docker-dependent features simply report
		// themselves as unavailable. An unreachable-but-valid host is
		// handled the same way, surfaced per-request via Status().
		slog.Warn("docker client unavailable, Docker-dependent features will be disabled", "error", err)
		dockerClient = nil
	}

	staticFS, err := web.DistFS()
	if err != nil {
		return err
	}

	server := api.NewServer(cfg, db, dockerClient, staticFS)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Long-lived Server-Sent Event streams never complete on their own, and
	// http.Server.Shutdown only waits for connections to go idle — without
	// this, a single open browser tab would stall shutdown until the timeout
	// below and make the process exit non-zero. RegisterOnShutdown lets the
	// SSE handlers return as soon as graceful shutdown begins.
	httpServer.RegisterOnShutdown(server.Shutdown)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("back-orbit listening", "addr", cfg.HTTPAddr, "dataDir", cfg.DataDir)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if dockerClient != nil {
			_ = dockerClient.Close()
		}
	}

	return nil
}
