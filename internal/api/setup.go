package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
)

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	hasUser, err := s.users.HasAnyUser(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setupComplete": hasUser})
}

type setupAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	var req setupAdminRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, http.StatusBadRequest, "username must be between 3 and 64 characters")
		return
	}
	if len(req.Password) < minAdminPasswordLn {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}

	user, err := s.users.CreateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrUserAlreadySetUp) {
			writeError(w, http.StatusConflict, "an administrator account already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create administrator account")
		return
	}

	s.recorder.Emit(r.Context(), events.Event{
		Action:      events.ActionAdminAccountCreated,
		ActorUserID: user.ID,
		TargetType:  "user",
		TargetID:    user.ID,
		Metadata:    map[string]any{"username": user.Username},
	})

	session, err := s.sessions.Create(r.Context(), user.ID, remoteIP(r), r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "account created, but failed to start a session; please log in")
		return
	}
	auth.SetSessionCookie(w, s.cfg.SessionCookieName, session.Token, session.ExpiresAt, requestIsSecure(r, s.cfg.TrustProxyHeaders))

	writeJSON(w, http.StatusCreated, userResponse(user, session.ExpiresAt))
}

func userResponse(user auth.User, sessionExpiresAt time.Time) map[string]any {
	return map[string]any{
		"id":               user.ID,
		"username":         user.Username,
		"sessionExpiresAt": sessionExpiresAt,
	}
}
