// Package projects manages the registry of Docker Compose projects Back-Orbit
// knows about, bridging live Docker discovery (internal/docker) with
// persisted project records that later phases (backup plans, snapshots)
// attach to.
package projects

import (
	"errors"
	"time"

	"github.com/back-orbit/back-orbit/internal/docker"
)

var (
	ErrProjectExists   = errors.New("projects: a project with this name already exists")
	ErrProjectNotFound = errors.New("projects: project not found")
	ErrInvalidPath     = errors.New("projects: project path must be an absolute path")
)

// Source records how a project entered Back-Orbit's registry.
type Source string

const (
	SourceDiscovered Source = "discovered"
	SourceRegistered Source = "registered"
)

// Status is a coarse, user-facing project health indicator. It is
// deliberately limited to the values the UI knows how to render distinctly.
type Status string

const (
	StatusHealthy     Status = "healthy"
	StatusWarning     Status = "warning"
	StatusFailed      Status = "failed"
	StatusRunning     Status = "running"
	StatusPaused      Status = "paused"
	StatusUnprotected Status = "unprotected"
)

// Record is a persisted project entry.
type Record struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ComposePath  string    `json:"composePath"`
	ComposeFiles []string  `json:"composeFiles"`
	Source       Source    `json:"source"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Detail is a project record enriched with live Docker state, when
// available. DockerAvailable is false whenever the Docker daemon could not
// be reached or the project has no currently-known containers; callers
// should render this as a warning rather than an error, since a project can
// legitimately be registered but not currently running.
type Detail struct {
	Record
	DockerAvailable bool               `json:"dockerAvailable"`
	Containers      []docker.Container `json:"containers"`
	Volumes         []docker.Volume    `json:"volumes"`
	Networks        []docker.Network   `json:"networks"`
	DockerWarning   string             `json:"dockerWarning,omitempty"`
}
