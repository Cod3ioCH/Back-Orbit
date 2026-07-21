package api

import (
	"errors"
	"net/http"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

// handleSecretStoreStatus reports what the store needs, without ever
// revealing anything about the secrets it holds.
func (s *Server) handleSecretStoreStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.secrets.IsInitialized(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read secret store status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": initialized,
		"unlocked":    s.secrets.IsUnlocked(),
		// Whether an unattended unlock is configured is what tells an operator
		// if backups will survive the next restart, so it belongs in the
		// status rather than only in the logs.
		"unattendedUnlockConfigured": s.cfg.MasterKeyFile != "",
	})
}

type passphraseRequest struct {
	Passphrase string `json:"passphrase"`
}

func (s *Server) handleSecretStoreInitialize(w http.ResponseWriter, r *http.Request) {
	var req passphraseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.secrets.Initialize(r.Context(), req.Passphrase); err != nil {
		switch {
		case errors.Is(err, secrets.ErrAlreadyInitialized):
			writeError(w, http.StatusConflict, "the secret store is already initialised")
		default:
			// The only other failure here is the length rule, whose message is
			// safe and useful to show.
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	s.recorder.Emit(r.Context(), events.Event{
		Action:      events.ActionSecretStoreInitialized,
		ActorUserID: user.ID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSecretStoreUnlock(w http.ResponseWriter, r *http.Request) {
	var req passphraseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.secrets.Unlock(r.Context(), req.Passphrase); err != nil {
		switch {
		case errors.Is(err, secrets.ErrNotInitialized):
			writeError(w, http.StatusConflict, "the secret store has not been set up yet")
		case errors.Is(err, secrets.ErrInvalidPassphrase):
			writeError(w, http.StatusUnauthorized, "incorrect master passphrase")
		default:
			writeError(w, http.StatusInternalServerError, "failed to unlock the secret store")
		}
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	s.recorder.Emit(r.Context(), events.Event{
		Action:      events.ActionSecretStoreUnlocked,
		ActorUserID: user.ID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSecretStoreLock(w http.ResponseWriter, r *http.Request) {
	s.secrets.Lock()

	user, _ := auth.UserFromContext(r.Context())
	s.recorder.Emit(r.Context(), events.Event{
		Action:      events.ActionSecretStoreLocked,
		ActorUserID: user.ID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleListSecrets returns metadata only. The store's List returns a type
// that has no field for a value, so there is no route from this handler to a
// plaintext secret even by mistake.
func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	list, err := s.secrets.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
