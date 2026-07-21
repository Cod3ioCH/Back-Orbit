package api

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/config"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/projectanalyzer"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
	"github.com/Cod3ioCH/Back-Orbit/internal/repositories"
	restorejobs "github.com/Cod3ioCH/Back-Orbit/internal/restore"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
	"github.com/Cod3ioCH/Back-Orbit/internal/storage"
)

// loginRateLimit configures the login brute-force protection: 5 failed
// attempts per 15-minute window trigger exponential backoff, capped at 1
// hour.
const (
	loginMaxAttempts   = 5
	loginWindow        = 15 * time.Minute
	loginMaxBackoff    = 1 * time.Hour
	minAdminPasswordLn = 12
)

// Server holds every dependency the HTTP API needs and knows how to build
// the router. It has no global state: everything flows through this struct,
// constructed once in cmd/back-orbit/main.go.
type Server struct {
	cfg  config.Config
	auth *auth.Authenticator

	users       *auth.UserStore
	sessions    *auth.SessionStore
	rateLimiter *auth.LoginRateLimiter

	dockerClient docker.Client
	projects     *projects.Service
	analyzer     *projectanalyzer.Service

	secrets      *secrets.Store
	repositories *repositories.Service
	backups      *backuprun.Runner
	restores     *restorejobs.Runner

	eventStore  *events.Store
	eventBroker *events.Broker
	recorder    *events.Recorder

	staticFS fs.FS
	db       *sql.DB

	// shutdown is closed once (via Shutdown) to tell long-lived handlers —
	// currently the SSE activity stream — to return, so the HTTP server's
	// graceful Shutdown isn't blocked waiting on connections that never
	// complete on their own.
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// NewServer wires up a Server from its dependencies. dockerClient may be
// nil (Docker discovery disabled); staticFS may be nil (no frontend build
// embedded, e.g. in local dev where Vite serves the frontend separately).
//
// secretStore is passed in rather than constructed here because startup may
// already have unlocked it from a key file, and that unlocked state must be
// the same object the API serves from.
func NewServer(cfg config.Config, db *sql.DB, dockerClient docker.Client, secretStore *secrets.Store, staticFS fs.FS) *Server {
	users := auth.NewUserStore(db)
	sessions := auth.NewSessionStore(db, cfg.SessionTTL)

	eventStore := events.NewStore(db)
	eventBroker := events.NewBroker()
	recorder := events.NewRecorder(eventStore, eventBroker)

	engine := backup.NewResticEngine("")

	projectService := projects.NewService(db, dockerClient, recorder)
	analyzerService := projectanalyzer.NewService(db, projectService, dockerClient, recorder)
	repositoryService := repositories.NewService(db, secretStore, engine, recorder,
		repositories.NewLocations(cfg.DataDir, cfg.BackupDir))

	// Staging materialises a full copy of each volume, so it lives beside the
	// database rather than in the backup destination: a half-staged copy must
	// never end up somewhere that looks like a backup.
	stager := storage.NewStager(dockerClient, "")

	// The runner reads the analyzer's blueprint so a backup can say which
	// detected databases it captured as plain files rather than as a dump.
	backupRunner := backuprun.NewRunner(db, projectService, repositoryService, stager, engine, recorder,
		filepath.Join(cfg.DataDir, "staging"), analyzerService, dockerClient)
	return &Server{
		cfg: cfg,
		auth: &auth.Authenticator{
			Sessions:   sessions,
			Users:      users,
			CookieName: cfg.SessionCookieName,
		},
		users:        users,
		sessions:     sessions,
		rateLimiter:  auth.NewLoginRateLimiter(loginMaxAttempts, loginWindow, loginMaxBackoff),
		dockerClient: dockerClient,
		projects:     projectService,
		analyzer:     analyzerService,
		secrets:      secretStore,
		repositories: repositoryService,
		backups:      backupRunner,
		restores: restorejobs.NewRunner(db, backupRunner, repositoryService, engine, recorder,
			filepath.Join(cfg.DataDir, "restores"), dockerClient),
		eventStore:  eventStore,
		eventBroker: eventBroker,
		recorder:    recorder,
		staticFS:    staticFS,
		db:          db,
		shutdown:    make(chan struct{}),
	}
}

// CloseInterruptedRuns fails any backup left marked running by a process that
// stopped mid-run, and reports how many were closed out.
//
// Nothing else can: a run lives on a goroutine, so a killed process leaves the
// row claiming to be in progress forever. That reads as "a backup is running",
// which is the most misleading thing this UI could say about a backup that
// never finished.
func (s *Server) CloseInterruptedRuns(ctx context.Context) (int64, error) {
	backups, err := s.backups.CloseInterruptedRuns(ctx)
	if err != nil {
		return 0, err
	}
	restores, err := s.restores.CloseInterruptedRuns(ctx)
	return backups + restores, err
}

// Shutdown signals long-lived handlers (the SSE activity stream) to stop.
// It is safe to call more than once. Wire it into the HTTP server via
// http.Server.RegisterOnShutdown so it fires as part of graceful shutdown.
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdown) })
}

// Router builds the complete HTTP handler: middleware chain, API routes, and
// (if configured) the embedded frontend.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(requestLogger)
	r.Use(recoverer)
	r.Use(auth.SecurityHeaders(s.cfg.TrustProxyHeaders))
	r.Use(s.ensureCSRFCookie)

	r.Get("/healthz", s.handleHealthz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.CSRFProtect)

		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup/admin", s.handleSetupAdmin)

		r.Post("/auth/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.auth.RequireSession)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/session", s.handleSession)

			r.Get("/docker/status", s.handleDockerStatus)

			r.Get("/projects", s.handleListProjects)
			r.Post("/projects", s.handleRegisterProject)
			r.Post("/projects/scan", s.handleScanProjects)
			r.Get("/projects/{id}", s.handleGetProject)
			r.Delete("/projects/{id}", s.handleDeleteProject)
			r.Get("/projects/{id}/blueprint", s.handleGetProjectBlueprint)
			r.Post("/projects/{id}/analyze", s.handleAnalyzeProject)
			r.Post("/projects/{id}/blueprint/confirm", s.handleConfirmProjectBlueprint)
			r.Post("/projects/{id}/backup", s.handleStartBackup)

			r.Route("/backups", func(r chi.Router) {
				r.Get("/", s.handleListBackupRuns)
				r.Get("/{id}", s.handleGetBackupRun)
				r.Post("/{id}/cancel", s.handleCancelBackupRun)
			})
			r.Route("/restores", func(r chi.Router) {
				r.Get("/", s.handleListRestoreRuns)
				r.Post("/preview", s.handlePreviewRestore)
				r.Post("/", s.handleStartRestore)
				r.Post("/database", s.handleRestoreDatabase)
				r.Get("/{id}", s.handleGetRestoreRun)
				r.Post("/{id}/cancel", s.handleCancelRestoreRun)
			})

			r.Route("/secrets", func(r chi.Router) {
				r.Get("/status", s.handleSecretStoreStatus)
				r.Post("/initialize", s.handleSecretStoreInitialize)
				r.Post("/unlock", s.handleSecretStoreUnlock)
				r.Post("/lock", s.handleSecretStoreLock)
				r.Get("/", s.handleListSecrets)
			})

			r.Route("/repositories", func(r chi.Router) {
				r.Get("/", s.handleListRepositories)
				r.Post("/", s.handleCreateRepository)
				// Registered before "/{id}" so it is not read as an ID.
				r.Get("/locations", s.handleRepositoryLocations)
				r.Get("/{id}", s.handleGetRepository)
				r.Delete("/{id}", s.handleDeleteRepository)
				r.Post("/{id}/check", s.handleCheckRepository)
				r.Post("/{id}/initialize", s.handleInitializeRepository)
			})

			r.Get("/audit", s.handleListAudit)
			r.Get("/activity/stream", s.handleActivityStream)
		})
	})

	s.mountStatic(r)

	return r
}

func (s *Server) ensureCSRFCookie(next http.Handler) http.Handler {
	isSecure := func(r *http.Request) bool { return requestIsSecure(r, s.cfg.TrustProxyHeaders) }
	return auth.EnsureCSRFCookie(isSecure)(next)
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("http handler panic", "error", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
