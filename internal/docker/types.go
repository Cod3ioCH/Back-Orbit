// Package docker discovers Docker Compose projects on the configured Docker
// host. All access to the Docker daemon goes through the Client interface so
// callers can be tested against a fake implementation, and so a future
// remote-host transport (see ADR-0011) can implement the same interface
// without changing any caller.
package docker

import (
	"context"
	"errors"
	"time"
)

// Compose label keys, as set by Docker Compose on every container, volume,
// and network it creates or attaches. See
// https://docs.docker.com/reference/compose-file/ for the authoritative
// list.
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelWorkingDir  = "com.docker.compose.project.working_dir"
	LabelConfigFiles = "com.docker.compose.project.config_files"
)

// ErrProjectNotFound is returned by GetComposeProject when no containers
// carry the requested project label.
var ErrProjectNotFound = errors.New("docker: compose project not found")

// MountType mirrors the Docker mount types relevant to Back-Orbit.
type MountType string

const (
	MountTypeVolume MountType = "volume"
	MountTypeBind   MountType = "bind"
	MountTypeTmpfs  MountType = "tmpfs"
	MountTypeOther  MountType = "other"
)

// Mount describes a single mount point of a container.
type Mount struct {
	Type        MountType `json:"type"`
	Name        string    `json:"name,omitempty"` // volume name, only set when Type == MountTypeVolume
	Source      string    `json:"source"`         // host path for bind mounts, storage location for volumes
	Destination string    `json:"destination"`
	ReadOnly    bool      `json:"readOnly"`
}

// Container is a condensed view of a Docker container relevant to backup
// discovery.
type Container struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Service   string            `json:"service,omitempty"`
	Image     string            `json:"image"`
	ImageID   string            `json:"imageId"`
	State     string            `json:"state"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"createdAt"`
	Labels    map[string]string `json:"labels,omitempty"`
	Mounts    []Mount           `json:"mounts"`
}

// Volume is a condensed view of a Docker named volume.
type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// Network is a condensed view of a Docker network.
type Network struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels,omitempty"`
}

// ComposeProject groups the containers, volumes, and networks that belong to
// one Docker Compose project, as identified by the com.docker.compose.project
// label.
type ComposeProject struct {
	Name        string      `json:"name"`
	WorkingDir  string      `json:"workingDir"`
	ConfigFiles []string    `json:"configFiles"`
	Containers  []Container `json:"containers"`
	Volumes     []Volume    `json:"volumes"`
	Networks    []Network   `json:"networks"`
}

// Status reports the reachability of the configured Docker host, along with
// the security notice Back-Orbit always surfaces about socket access.
type Status struct {
	Connected     bool   `json:"connected"`
	Host          string `json:"host"`
	ServerVersion string `json:"serverVersion,omitempty"`
	APIVersion    string `json:"apiVersion,omitempty"`
	Error         string `json:"error,omitempty"`
}

// SecurityNotice is a fixed, user-facing explanation of the privilege
// implications of Docker access, always returned alongside Status so the UI
// can show it verbatim. See docs/threat-model.md.
const SecurityNotice = "Back-Orbit connects to the Docker daemon to discover and manage Compose projects. Access to the Docker socket is equivalent to root access on this host. Review docs/threat-model.md and consider running behind a Docker Socket Proxy in production."

// Client abstracts Docker daemon access so it can be faked in tests and so a
// future remote-host implementation can satisfy the same interface (see
// ADR-0011).
type Client interface {
	// Status reports whether the Docker daemon is reachable.
	Status(ctx context.Context) Status

	// ListComposeProjects returns every Compose project with at least one
	// container known to the Docker daemon (running or stopped).
	ListComposeProjects(ctx context.Context) ([]ComposeProject, error)

	// GetComposeProject returns a single Compose project by name, or
	// ErrProjectNotFound if no containers carry that project label.
	GetComposeProject(ctx context.Context, name string) (ComposeProject, error)

	// Close releases any resources held by the client.
	Close() error
}
