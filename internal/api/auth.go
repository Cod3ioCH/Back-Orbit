package api

import (
	"errors"
	"net/http"

	"github.com/back-orbit/back-orbit/internal/auth"
	"github.com/back-orbit/back-orbit/internal/events"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rateLimitKey := remoteIP(r) + ":" + req.Username
	if !s.rateLimiter.Allow(rateLimitKey) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts, please try again later")
		return
	}

	user, err := s.users.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		s.rateLimiter.RecordFailure(rateLimitKey)
		s.recorder.Emit(r.Context(), events.Event{
			Action:   events.ActionLoginFailed,
			Metadata: map[string]any{"username": req.Username},
		})
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	s.rateLimiter.RecordSuccess(rateLimitKey)

	session, err := s.sessions.Create(r.Context(), user.ID, remoteIP(r), r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	auth.SetSessionCookie(w, s.cfg.SessionCookieName, session.Token, session.ExpiresAt, requestIsSecure(r, s.cfg.TrustProxyHeaders))

	s.recorder.Emit(r.Context(), events.Event{
		Action:      events.ActionLoginSucceeded,
		ActorUserID: user.ID,
		Metadata:    map[string]any{"username": user.Username},
	})

	writeJSON(w, http.StatusOK, userResponse(user, session.ExpiresAt))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := auth.TokenFromCookie(r, s.cfg.SessionCookieName)
	if token != "" {
		_ = s.sessions.Delete(r.Context(), token)
	}
	auth.ClearSessionCookie(w, s.cfg.SessionCookieName, requestIsSecure(r, s.cfg.TrustProxyHeaders))

	if user, ok := auth.UserFromContext(r.Context()); ok {
		s.recorder.Emit(r.Context(), events.Event{
			Action:      events.ActionLogout,
			ActorUserID: user.ID,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	session, _ := auth.SessionFromContext(r.Context())
	writeJSON(w, http.StatusOK, userResponse(user, session.ExpiresAt))
}
