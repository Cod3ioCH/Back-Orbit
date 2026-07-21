package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/repositories"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	list, err := s.repositories.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repositories")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRepository(w http.ResponseWriter, r *http.Request) {
	repo, err := s.repositories.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

// handleRepositoryLocations reports where a local repository can be stored on
// this installation, so the UI can offer a working path instead of asking the
// operator to guess one that the container can actually write to.
func (s *Server) handleRepositoryLocations(w http.ResponseWriter, r *http.Request) {
	locations := s.repositories.Locations()
	if locations == nil {
		locations = []repositories.Location{}
	}
	writeJSON(w, http.StatusOK, locations)
}

type createRepositoryRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Location string `json:"location"`
	Password string `json:"password"`
}

func (s *Server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	var req createRepositoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	repo, err := s.repositories.Create(r.Context(), user.ID, repositories.CreateRequest{
		Name:     req.Name,
		Kind:     backup.RepositoryKind(req.Kind),
		Location: req.Location,
		Password: req.Password,
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	if err := s.repositories.Delete(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	result, err := s.repositories.Check(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleInitializeRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	if err := s.repositories.Initialize(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeRepositoryError maps domain failures to status codes. A locked secret
// store gets its own code and message because it is the one failure here the
// operator can fix immediately, and a generic 500 would hide that.
// unwrapMessage strips a sentinel's own wording from a wrapped error, leaving
// the explanation that was written for a human.
//
// Errors are wrapped as "<sentinel>: <explanation>" so callers can match on
// the sentinel with errors.Is. That prefix is for code, not for the person
// looking at the form, and showing it makes a clear sentence look like a crash.
// If no sentinel matches the prefix the message is returned untouched, so this
// can never silently drop the only text there is.
func unwrapMessage(err error, sentinels ...error) string {
	message := err.Error()
	for _, sentinel := range sentinels {
		prefix := sentinel.Error() + ": "
		if strings.HasPrefix(message, prefix) {
			return strings.TrimPrefix(message, prefix)
		}
	}
	return message
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found")
	case errors.Is(err, repositories.ErrNameTaken):
		writeError(w, http.StatusConflict, "a repository with this name already exists")
	case errors.Is(err, repositories.ErrInvalidKind), errors.Is(err, repositories.ErrInvalidConfig):
		// The sentinel's own text names a Go package and a category, neither of
		// which means anything to the person reading it. Only the part that was
		// wrapped around it explains what to change.
		writeError(w, http.StatusBadRequest, unwrapMessage(err,
			repositories.ErrInvalidKind, repositories.ErrInvalidConfig))
	case errors.Is(err, secrets.ErrLocked):
		writeError(w, http.StatusConflict,
			"the secret store is locked, so repository passwords cannot be read; unlock it and try again")
	default:
		// Engine failures carry redacted output already, and the operator
		// needs the detail to act on it.
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
