// Package backuprun turns the separate pieces — staging a Docker volume, the
// restic engine, the secret store — into one backup that either demonstrably
// happened or demonstrably did not.
//
// The distinction this package exists to enforce is between a backup that was
// *reported* and one that was *verified*. restic exiting zero means it believes
// it wrote a snapshot. A snapshot row is only recorded here once that snapshot
// has been read back from the repository, because the alternative is a UI full
// of green ticks whose first real test is the day the data is needed.
package backuprun

import (
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/storage"
)

// Status is where a run ended up.
type Status string

const (
	StatusRunning Status = "running"
	// StatusCompletedWithWarnings is deliberately distinct from success: the
	// snapshot exists and verified, but something could not be captured
	// faithfully, and collapsing that into "completed" hides it until a
	// restore surfaces it the hard way.
	StatusCompletedWithWarnings Status = "completed_with_warnings"
	StatusCompleted             Status = "completed"
	StatusFailed                Status = "failed"
	StatusCancelled             Status = "cancelled"
)

// Done reports whether the run has finished, whatever the outcome.
func (s Status) Done() bool { return s != StatusRunning }

// Phase is what a running backup is currently doing. These are the names the
// operator sees, so they describe the work rather than the implementation.
type Phase string

const (
	PhasePreparing    Phase = "preparing"
	PhaseStaging      Phase = "staging"
	PhaseSnapshotting Phase = "snapshotting"
	PhaseVerifying    Phase = "verifying"
	PhaseFinished     Phase = "finished"
)

// Trigger records what started a run.
type Trigger string

const (
	TriggerManual    Trigger = "manual"
	TriggerScheduled Trigger = "scheduled"
)

// Run is one backup attempt, kept whether it succeeded or not.
type Run struct {
	ID string `json:"id"`

	ProjectID      string `json:"projectId,omitempty"`
	ProjectName    string `json:"projectName"`
	RepositoryID   string `json:"repositoryId,omitempty"`
	RepositoryName string `json:"repositoryName"`

	Trigger Trigger `json:"trigger"`
	Status  Status  `json:"status"`
	Phase   Phase   `json:"phase"`

	Volumes []string `json:"volumes"`

	FilesTotal int64 `json:"filesTotal"`
	BytesTotal int64 `json:"bytesTotal"`
	BytesAdded int64 `json:"bytesAdded"`

	Warnings []string `json:"warnings"`
	Error    string   `json:"error,omitempty"`

	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`

	// Snapshot is the verified snapshot this run produced, when it produced
	// one. A run without it did not leave a usable backup behind.
	Snapshot *Snapshot `json:"snapshot,omitempty"`
}

// Snapshot is a verified backup in a repository.
type Snapshot struct {
	ID           string `json:"id"`
	RunID        string `json:"runId"`
	RepositoryID string `json:"repositoryId,omitempty"`

	// ResticSnapshotID is what a restore is performed with — including a
	// restore done with plain restic, without Back-Orbit involved at all.
	ResticSnapshotID string `json:"resticSnapshotId"`

	Manifest Manifest `json:"manifest"`

	SizeBytes  int64 `json:"sizeBytes"`
	FilesCount int64 `json:"filesCount"`

	VerifiedAt   *time.Time   `json:"verifiedAt,omitempty"`
	Verification Verification `json:"verification"`

	CreatedAt time.Time `json:"createdAt"`
}

// ManifestSchemaVersion is incremented only for changes that older readers
// cannot tolerate. Fields are added, never repurposed, and unknown fields are
// ignored on read (ADR-0008), so a snapshot taken today stays readable by a
// much later version — which matters more here than almost anywhere, because
// the reader may be a disaster recovery years from now.
const ManifestSchemaVersion = 1

// Manifest describes what a snapshot contains and how to put it back.
type Manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Project       string    `json:"project"`
	CreatedAt     time.Time `json:"createdAt"`

	// Volumes carries one entry per named volume in the snapshot.
	Volumes []VolumeManifest `json:"volumes"`
}

// VolumeManifest is one volume's contribution to a snapshot.
type VolumeManifest struct {
	Name string `json:"name"`
	// PathInSnapshot is where this volume's contents sit inside the snapshot,
	// which is what a restore needs in order to find them again.
	PathInSnapshot string `json:"pathInSnapshot"`

	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`

	// Ownership records the original uid/gid/mode of every path.
	//
	// Back-Orbit stages volumes unprivileged, so it cannot apply the original
	// ownership to the staged copy — restic then records Back-Orbit's own uid
	// instead. Without this list a restore would hand an application files it
	// does not own, which is enough to stop a database from starting. It is
	// the difference between restoring the bytes and restoring the volume.
	Ownership []storage.OwnershipEntry `json:"ownership"`

	// OwnershipPreserved reports whether the staged files carried their
	// original ownership on disk. When false, Ownership is the only record of
	// it and a restore has to reapply it.
	OwnershipPreserved bool `json:"ownershipPreserved"`

	Warnings []string `json:"warnings,omitempty"`
}

// Verification records what was checked after the snapshot was written, so
// "verified" can be read back as a specific claim rather than a badge.
type Verification struct {
	OK          bool     `json:"ok"`
	Checks      []string `json:"checks,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	FilesListed int64    `json:"filesListed"`
	// BytesInSnapshot is what restoring this snapshot would produce.
	BytesInSnapshot int64 `json:"bytesInSnapshot"`
	DurationMS      int64 `json:"durationMs"`
}
