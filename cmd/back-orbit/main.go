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

	"github.com/Cod3ioCH/Back-Orbit/internal/api"
	"github.com/Cod3ioCH/Back-Orbit/internal/config"
	"github.com/Cod3ioCH/Back-Orbit/internal/database"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
	"github.com/Cod3ioCH/Back-Orbit/web"
)

// unlockSecretStore opens the secret store at startup when a master key file
// is configured, so scheduled work can reach repository credentials without a
// human being present after a restart.
//
// Every outcome here is a warning rather than a fatal error: Back-Orbit still
// serves its UI with a locked store, and refusing to start would take the
// whole tool offline over a problem the operator can only diagnose through
// that same UI. What must never happen is failing silently — each case says
// plainly what is not going to work until it is fixed.
func unlockSecretStore(ctx context.Context, cfg config.Config, store *secrets.Store) {
	initialized, err := store.IsInitialized(ctx)
	if err != nil {
		slog.Error("could not determine secret store state", "error", err)
		return
	}
	if !initialized {
		slog.Info("secret store not initialised yet; set a master passphrase to store credentials")
		return
	}

	if cfg.MasterKeyFile == "" {
		slog.Warn("secret store is locked and no master key file is configured; " +
			"scheduled backups cannot run until it is unlocked")
		return
	}

	if err := store.UnlockFromFile(ctx, cfg.MasterKeyFile); err != nil {
		slog.Error("could not unlock the secret store from the master key file; "+
			"scheduled backups will not run until this is resolved",
			"path", cfg.MasterKeyFile, "error", err)
		return
	}

	slog.Info("secret store unlocked from master key file", "path", cfg.MasterKeyFile)
}

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

	secretStore := secrets.NewStore(db)
	unlockSecretStore(context.Background(), cfg, secretStore)

	staticFS, err := web.DistFS()
	if err != nil {
		return err
	}

	server := api.NewServer(cfg, db, dockerClient, secretStore, staticFS)

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
