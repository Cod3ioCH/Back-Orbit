// Package repositories manages backup destinations: where snapshots are
// written, whether they are reachable, and — via the secret store — the
// credentials needed to open them.
//
// A repository's password never appears in this package's types. It is held
// in the encrypted secret store and fetched just in time when an operation
// actually needs to talk to the repository, so there is no struct here that
// could carry it into an API response by accident.
package repositories

import (
	"errors"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
)

var (
	ErrNotFound      = errors.New("repositories: repository not found")
	ErrNameTaken     = errors.New("repositories: a repository with this name already exists")
	ErrInvalidKind   = errors.New("repositories: unsupported repository kind")
	ErrInvalidConfig = errors.New("repositories: invalid repository configuration")
)

// Status describes what Back-Orbit last observed about a repository. It is a
// cached observation, not a live fact: it says what happened at
// LastCheckedAt, which is why the UI shows that timestamp alongside it.
type Status string

const (
	// StatusUnknown means the repository has never been checked.
	StatusUnknown Status = "unknown"
	// StatusUninitialized means the destination is reachable but holds no
	// repository yet, so it needs initialising before anything can be written.
	StatusUninitialized Status = "uninitialized"
	// StatusReady means the repository was reachable and readable.
	StatusReady Status = "ready"
	// StatusError means the last check failed; LastError explains how.
	StatusError Status = "error"
)

// Repository is a backup destination as Back-Orbit knows it.
type Repository struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Kind     backup.RepositoryKind `json:"kind"`
	Location string                `json:"location"`

	Status        Status     `json:"status"`
	LastError     string     `json:"lastError,omitempty"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CheckResult reports what a connection test found.
type CheckResult struct {
	Status Status `json:"status"`
	// SnapshotCount is only meaningful when Status is StatusReady.
	SnapshotCount int    `json:"snapshotCount"`
	Error         string `json:"error,omitempty"`
}
