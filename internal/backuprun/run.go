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
	"encoding/json"
	"fmt"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbdump"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
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

	// VerifyRestores asks each export to be loaded back into a throwaway
	// server before the run is called done. Not persisted: it describes what
	// this run was asked to do, and the outcome is recorded per database.
	VerifyRestores bool `json:"-"`

	// sources carries the resolved sources between Start and the goroutine
	// that runs the backup. Not persisted: the run row records the names, and
	// this is only the plumbing to reach them.
	sources []projects.BackupSource

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

	// Databases lists the logical exports in this snapshot. A restore reads
	// these instead of the raw files underneath, which is the whole reason the
	// dump was taken.
	// Always present, even when empty: "this snapshot contains no databases"
	// is an answer, and a missing field is not.
	Databases []DatabaseDump `json:"databases"`
}

// ProtectionLevel says how well a database in a snapshot can be brought back.
//
// The distinction is the whole point of the analyzer and the exporters. A
// snapshot that does not carry it forces whoever restores to guess, and the
// moment they are guessing is the moment they can least afford to.
type ProtectionLevel string

const (
	// ProtectionExported is a logical dump: the database described itself, and
	// the export can be replayed into any compatible server.
	ProtectionExported ProtectionLevel = "exported"
	// ProtectionConsistent is a file captured through the engine's own backup
	// API — coherent, but still the engine's own format. SQLite lands here.
	ProtectionConsistent ProtectionLevel = "consistent"
	// ProtectionFilesOnly is a plain copy of a data directory. It restores if
	// the service was stopped, and may not if it was running.
	ProtectionFilesOnly ProtectionLevel = "files_only"
)

// DatabaseDump is one database's presence in a snapshot, and how well it is
// protected there.
type DatabaseDump struct {
	Technology string          `json:"technology"`
	Service    string          `json:"service"`
	Level      ProtectionLevel `json:"level"`
	// Path is where the export sits inside the snapshot. Empty when the
	// database was only copied as files.
	Path string `json:"path,omitempty"`
	// Command is what produced the export, so a restore does not have to guess
	// how to read the file back.
	Command string `json:"command,omitempty"`
	// User is the account the export was taken as, and the one a replay should
	// use. Never a password.
	User  string `json:"user,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
	// Note explains a level that is not "exported", so the limitation travels
	// with the snapshot rather than living only in a run's warnings.
	Note string `json:"note,omitempty"`

	// RestoreCheck is the outcome of loading this export back into an empty
	// server, when that was asked for. Its absence means the export was never
	// tried, which is different from having been tried and failed.
	RestoreCheck *dbdump.RestoreCheck `json:"restoreCheck,omitempty"`
}

// MarshalJSON adds the replay command to the serialised form.
//
// Computed here rather than in the frontend so there is one source of truth
// for how a dump is put back. A command the UI assembles itself would drift
// from the one the exporter actually produced.
// MarshalJSON keeps every list a list.
func (v VolumeManifest) MarshalJSON() ([]byte, error) {
	type plain VolumeManifest
	copied := plain(v)
	copied.Ownership = nonNil(copied.Ownership)
	copied.SQLiteDatabases = nonNil(copied.SQLiteDatabases)
	copied.Warnings = nonNil(copied.Warnings)
	return json.Marshal(copied)
}

// MarshalJSON keeps every list a list.
func (m Manifest) MarshalJSON() ([]byte, error) {
	type plain Manifest
	copied := plain(m)
	copied.Volumes = nonNil(copied.Volumes)
	copied.Databases = nonNil(copied.Databases)
	return json.Marshal(copied)
}

func (d DatabaseDump) MarshalJSON() ([]byte, error) {
	type plain DatabaseDump
	return json.Marshal(struct {
		plain
		Replay string `json:"replay,omitempty"`
	}{plain: plain(d), Replay: d.Replay()})
}

// Replay returns the command that puts this export back.
//
// A dump inside a snapshot is a file until someone knows what to do with it.
// This is written for the person standing in front of a broken system, so it
// names the service rather than a container id, and prompts for the password
// rather than carrying one.
func (d DatabaseDump) Replay() string {
	if d.Level != ProtectionExported || d.Path == "" {
		return ""
	}
	user := d.User
	switch d.Technology {
	case "postgresql":
		if user == "" {
			user = "postgres"
		}
		return fmt.Sprintf("docker compose exec -T %s psql -U %s < %s", d.Service, user, d.Path)
	case "mysql", "mariadb":
		client := "mysql"
		if d.Technology == "mariadb" {
			client = "mariadb"
		}
		if user == "" {
			user = "root"
		}
		// -p without a value makes the client prompt, so no password is
		// written down anywhere.
		return fmt.Sprintf("docker compose exec -T %s %s -u %s -p < %s", d.Service, client, user, d.Path)
	case "mongodb":
		// mongorestore reads the archive mongodump wrote. --drop replaces
		// existing collections, which is what restoring means.
		//
		// The exclusions are not tidiness. A mongodump archive contains the
		// admin database, and restoring it replaces the target server's
		// accounts with the source's — verified: the data came back, and then
		// nothing could authenticate except the credentials from the machine
		// the backup was taken on. mongodump has no --excludeDatabase, so the
		// exclusion belongs here, on the way back in.
		auth := ""
		if user != "" {
			auth = fmt.Sprintf(" -u %s --authenticationDatabase admin -p", user)
		}
		return fmt.Sprintf(
			"docker compose exec -T %s mongorestore --archive --drop "+
				"--nsExclude 'admin.*' --nsExclude 'config.*'%s < %s",
			d.Service, auth, d.Path)
	default:
		return ""
	}
}

// nonNil returns an empty slice instead of a nil one.
//
// A nil slice marshals to JSON null, and an API that returns null where it
// usually returns an array makes every consumer defend against both. This one
// did not: an empty volume produced a null ownership list and the snapshot
// details crashed on `.length`, blanking the page.
func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// VolumeManifest is one volume's contribution to a snapshot.
type VolumeManifest struct {
	Name string `json:"name"`
	// Kind is "volume" or "bind" — a restore has to put the data back the way
	// it came, and those are two different operations.
	Kind string `json:"kind"`
	// MountedAt is where this data appeared inside the application container.
	MountedAt string `json:"mountedAt,omitempty"`
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

	// SQLiteDatabases names the databases that were captured through SQLite
	// rather than copied as files, so a restore knows which files are a
	// consistent database and which are only a copy.
	SQLiteDatabases []storage.SQLiteCapture `json:"sqliteDatabases,omitempty"`

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
