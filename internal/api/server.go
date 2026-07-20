package api

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/back-orbit/back-orbit/internal/auth"
	"github.com/back-orbit/back-orbit/internal/config"
	"github.com/back-orbit/back-orbit/internal/docker"
	"github.com/back-orbit/back-orbit/internal/events"
	"github.com/back-orbit/back-orbit/internal/projects"
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

	eventStore  *events.Store
	eventBroker *events.Broker
	recorder    *events.Recorder

	staticFS fs.FS
	db       *sql.DB
}

// NewServer wires up a Server from its dependencies. dockerClient may be
// nil (Docker discovery disabled); staticFS may be nil (no frontend build
// embedded, e.g. in local dev where Vite serves the frontend separately).
func NewServer(cfg config.Config, db *sql.DB, dockerClient docker.Client, staticFS fs.FS) *Server {
	users := auth.NewUserStore(db)
	sessions := auth.NewSessionStore(db, cfg.SessionTTL)

	eventStore := events.NewStore(db)
	eventBroker := events.NewBroker()
	recorder := events.NewRecorder(eventStore, eventBroker)

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
		projects:     projects.NewService(db, dockerClient, recorder),
		eventStore:   eventStore,
		eventBroker:  eventBroker,
		recorder:     recorder,
		staticFS:     staticFS,
		db:           db,
	}
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
