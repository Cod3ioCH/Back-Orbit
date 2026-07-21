package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
)

func TestRequireSessionRejectsMissingCookie(t *testing.T) {
	db := dbtest.Open(t)
	authenticator := &Authenticator{
		Sessions:   NewSessionStore(db, time.Hour),
		Users:      NewUserStore(db),
		CookieName: "backorbit_session",
	}

	called := false
	handler := authenticator.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not run without a valid session")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireSessionAttachesUserAndSession(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	sessions := NewSessionStore(db, time.Hour)
	authenticator := &Authenticator{Sessions: sessions, Users: users, CookieName: "backorbit_session"}

	ctx := context.Background()
	user, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	session, err := sessions.Create(ctx, user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	var gotUser User
	var gotSession Session
	handler := authenticator.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, _ = UserFromContext(r.Context())
		gotSession, _ = SessionFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "backorbit_session", Value: session.Token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotUser.ID != user.ID {
		t.Fatalf("expected user %q in context, got %q", user.ID, gotUser.ID)
	}
	if gotSession.ID != session.ID {
		t.Fatalf("expected session %q in context, got %q", session.ID, gotSession.ID)
	}
}

func TestSecurityHeadersSetsExpectedHeaders(t *testing.T) {
	handler := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("expected X-Content-Type-Options: nosniff")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected a Content-Security-Policy header")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("expected no HSTS header when hstsEnabled is false")
	}
}

func TestSecurityHeadersSetsHSTSWhenEnabled(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("expected an HSTS header when hstsEnabled is true")
	}
}
