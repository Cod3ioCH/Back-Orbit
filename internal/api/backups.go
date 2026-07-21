package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
	"github.com/Cod3ioCH/Back-Orbit/internal/repositories"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

type startBackupRequest struct {
	RepositoryID string `json:"repositoryId"`
	// VerifyRestores asks each database export to be loaded back into a
	// throwaway server before the run is called done.
	VerifyRestores bool `json:"verifyRestores"`
}

// handleStartBackup begins a backup and returns immediately with the run, so
// the client can follow it rather than hold a request open for minutes.
func (s *Server) handleStartBackup(w http.ResponseWriter, r *http.Request) {
	var req startBackupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepositoryID == "" {
		writeError(w, http.StatusBadRequest, "a repository must be chosen")
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	run, err := s.backups.Start(r.Context(), backuprun.StartRequest{
		ProjectID:      chi.URLParam(r, "id"),
		RepositoryID:   req.RepositoryID,
		ActorUserID:    user.ID,
		Trigger:        backuprun.TriggerManual,
		VerifyRestores: req.VerifyRestores,
	})
	if err != nil {
		writeBackupError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListBackupRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.backups.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list backup runs")
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleGetBackupRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.backups.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleCancelBackupRun stops a running backup. Every long operation has to be
// abortable, and a backup is the longest one Back-Orbit performs.
func (s *Server) handleCancelBackupRun(w http.ResponseWriter, r *http.Request) {
	if err := s.backups.Cancel(chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, backuprun.ErrNotFound) {
			// Either the run finished on its own or it belongs to an earlier
			// process. Both mean there is nothing left to stop, and saying so
			// is more useful than a bare 404.
			writeError(w, http.StatusConflict, "this backup is no longer running, so there is nothing to cancel")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to cancel the backup")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backuprun.ErrNotFound), errors.Is(err, projects.ErrProjectNotFound),
		errors.Is(err, repositories.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, backuprun.ErrAlreadyRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, backuprun.ErrNothingToBackUp):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, secrets.ErrLocked):
		writeError(w, http.StatusConflict,
			"the secret store is locked, so the repository password cannot be read; unlock it and try again")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
