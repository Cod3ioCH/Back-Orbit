package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/projectanalyzer"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	records, err := s.projects.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleGetProjectBlueprint(w http.ResponseWriter, r *http.Request) {
	bp, err := s.analyzer.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, projectanalyzer.ErrBlueprintNotFound) {
			writeError(w, http.StatusNotFound, "project has not been analyzed")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load project blueprint")
		return
	}
	writeJSON(w, http.StatusOK, bp)
}

func (s *Server) handleAnalyzeProject(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	bp, err := s.analyzer.Analyze(r.Context(), chi.URLParam(r, "id"), user.ID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to analyze project")
		return
	}
	writeJSON(w, http.StatusOK, bp)
}

func (s *Server) handleConfirmProjectBlueprint(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	bp, err := s.analyzer.Confirm(r.Context(), chi.URLParam(r, "id"), user.ID)
	if err != nil {
		if errors.Is(err, projectanalyzer.ErrBlueprintNotFound) {
			writeError(w, http.StatusNotFound, "project has not been analyzed")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm project blueprint")
		return
	}
	writeJSON(w, http.StatusOK, bp)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	detail, err := s.projects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type registerProjectRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Server) handleRegisterProject(w http.ResponseWriter, r *http.Request) {
	var req registerProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		writeError(w, http.StatusBadRequest, "name must be between 1 and 128 characters")
		return
	}

	user, _ := auth.UserFromContext(r.Context())

	record, err := s.projects.Register(r.Context(), user.ID, req.Name, req.Path)
	if err != nil {
		switch {
		case errors.Is(err, projects.ErrInvalidPath):
			writeError(w, http.StatusBadRequest, "path must be an absolute filesystem path")
		case errors.Is(err, projects.ErrProjectExists):
			writeError(w, http.StatusConflict, "a project with this name already exists")
		default:
			writeError(w, http.StatusInternalServerError, "failed to register project")
		}
		return
	}

	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) handleScanProjects(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	records, err := s.projects.Scan(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"projects": records,
			"warning":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": records})
}
