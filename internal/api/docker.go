package api

import (
	"net/http"

	"github.com/back-orbit/back-orbit/internal/docker"
)

func (s *Server) handleDockerStatus(w http.ResponseWriter, r *http.Request) {
	if s.dockerClient == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected":      false,
			"error":          "Docker integration is not configured (BACKORBIT_DOCKER_HOST unset or unreachable at startup)",
			"securityNotice": docker.SecurityNotice,
		})
		return
	}

	status := s.dockerClient.Status(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"connected":      status.Connected,
		"host":           status.Host,
		"apiVersion":     status.APIVersion,
		"serverVersion":  status.ServerVersion,
		"error":          status.Error,
		"securityNotice": docker.SecurityNotice,
	})
}
