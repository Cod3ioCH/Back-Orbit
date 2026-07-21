package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/restore"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

func (s *Server) handlePreviewRestore(w http.ResponseWriter, r *http.Request) {
	var req restore.PreviewRequest
	if decodeJSON(w, r, &req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := s.restores.Preview(r.Context(), req)
	if err != nil {
		writeRestoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
func (s *Server) handleStartRestore(w http.ResponseWriter, r *http.Request) {
	var req restore.PreviewRequest
	if decodeJSON(w, r, &req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	run, err := s.restores.Start(r.Context(), restore.StartRequest{PreviewRequest: req, ActorUserID: u.ID})
	if err != nil {
		writeRestoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}
func (s *Server) handleListRestoreRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.restores.List(r.Context(), limit)
	if err != nil {
		writeError(w, 500, "failed to list restore runs")
		return
	}
	writeJSON(w, 200, runs)
}
func (s *Server) handleGetRestoreRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.restores.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeRestoreError(w, err)
		return
	}
	writeJSON(w, 200, run)
}
func (s *Server) handleCancelRestoreRun(w http.ResponseWriter, r *http.Request) {
	if err := s.restores.Cancel(chi.URLParam(r, "id")); err != nil {
		writeRestoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
func writeRestoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, restore.ErrNotFound), errors.Is(err, backuprun.ErrNotFound):
		writeError(w, 404, "not found")
	case errors.Is(err, restore.ErrUnsupported):
		writeError(w, 422, "this restore mode is not safe for the selected snapshot; run a preview for details")
	case errors.Is(err, restore.ErrAlreadyRunning):
		writeError(w, 409, err.Error())
	case errors.Is(err, restore.ErrNotConfirmed):
		writeError(w, 400, err.Error())
	case errors.Is(err, restore.ErrNoExport):
		writeError(w, 422, err.Error())
	case errors.Is(err, secrets.ErrLocked):
		writeError(w, 409,
			"the secret store is locked, so the snapshot cannot be read; unlock it and try again")
	default:
		writeError(w, 500, "restore operation failed")
	}
}

type restoreDatabaseRequest struct {
	SnapshotID string `json:"snapshotId"`
	Service    string `json:"service"`
	// Confirm must repeat the service name. This call replaces a live
	// database, and a confirmation only the UI enforces is a dialog, not a
	// safeguard — the API is reachable without it.
	Confirm string `json:"confirm"`
}

// handleRestoreDatabase replays one database's export back into the running
// service it came from, replacing what is there.
func (s *Server) handleRestoreDatabase(w http.ResponseWriter, r *http.Request) {
	var req restoreDatabaseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	run, err := s.restores.RestoreDatabase(r.Context(), restore.DatabaseRequest{
		SnapshotID:  req.SnapshotID,
		Service:     req.Service,
		Confirm:     req.Confirm,
		ActorUserID: user.ID,
	})
	if err != nil {
		writeRestoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}
