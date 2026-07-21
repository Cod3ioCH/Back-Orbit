// Package backup owns snapshot creation and restoration. All access to the
// underlying backup tool goes through the BackupEngine interface so the
// implementation (currently restic — see docs/adr/0002-restic-backup-engine.md)
// can be replaced without touching callers.
package backup

import (
	"context"
	"time"
)

// RepositoryKind identifies where a repository physically lives.
type RepositoryKind string

const (
	RepositoryLocal RepositoryKind = "local"
	RepositorySFTP  RepositoryKind = "sftp"
	RepositoryS3    RepositoryKind = "s3"
)

// RepositoryConfig describes a repository and how to authenticate to it.
//
// Password and the S3 credentials are secrets: they are passed to the backup
// tool through its environment, never as command-line arguments (which any
// local user could read via `ps`), and never written to logs. See
// redactEnvironment.
type RepositoryConfig struct {
	Kind RepositoryKind

	// Location is the repository path or URL, without the scheme prefix that
	// the engine adds itself (e.g. "/var/lib/back-orbit/repo",
	// "user@host:/srv/backups", "s3.example.com/bucket/prefix").
	Location string

	// Password unlocks the repository's own encryption. Losing it makes the
	// repository unreadable — by design, there is no recovery path.
	Password string

	// S3AccessKeyID and S3SecretAccessKey are only used for RepositoryS3.
	S3AccessKeyID     string
	S3SecretAccessKey string
}

// SnapshotRequest describes one backup run.
type SnapshotRequest struct {
	Repository RepositoryConfig

	// Paths are the absolute paths to back up.
	Paths []string

	// Excludes are restic exclude patterns applied to Paths.
	Excludes []string

	// Tags are attached to the snapshot so Back-Orbit can find its own
	// snapshots again (e.g. the project and plan they belong to).
	Tags []string

	// Host overrides the hostname recorded in the snapshot. Back-Orbit sets
	// this explicitly so snapshots stay attributable to a project even when
	// the container's hostname changes between runs.
	Host string

	// OnProgress, when set, is called as the backup advances. It must not
	// block: it runs on the goroutine reading the tool's output.
	OnProgress func(Progress)
}

// Progress reports how far a running backup has got. Fields are best-effort:
// the underlying tool does not know the totals until it has scanned.
type Progress struct {
	PercentDone  float64
	FilesDone    int64
	TotalFiles   int64
	BytesDone    int64
	TotalBytes   int64
	CurrentFiles []string
}

// SnapshotResult summarises a completed backup.
type SnapshotResult struct {
	SnapshotID string

	FilesNew        int64
	FilesChanged    int64
	FilesUnmodified int64

	DataAdded  int64
	TotalBytes int64
	TotalFiles int64

	Duration time.Duration

	// Warnings holds non-fatal problems, such as files that could not be
	// read. A snapshot with warnings still exists and is restorable, which is
	// why these are surfaced rather than turned into an error.
	Warnings []string
}

// Snapshot is an existing snapshot in a repository.
type Snapshot struct {
	ID      string
	ShortID string
	Time    time.Time
	Host    string
	Tags    []string
	Paths   []string
}

// RestoreRequest describes a restore into TargetPath.
type RestoreRequest struct {
	Repository RepositoryConfig

	// SnapshotID identifies the snapshot to restore; "latest" is accepted.
	SnapshotID string

	// TargetPath is the directory the snapshot is restored into.
	TargetPath string

	// Include, when non-empty, restores only these paths from the snapshot
	// instead of its entire contents.
	Include []string
}

// RestoreResult summarises a completed restore.
type RestoreResult struct {
	FilesRestored int64
	BytesRestored int64
	Duration      time.Duration
	Warnings      []string
}

// VerificationResult reports on a repository integrity check.
type VerificationResult struct {
	OK       bool
	Duration time.Duration
	// Errors holds the integrity problems found. OK is false when non-empty.
	Errors []string

	// Checks names what was actually examined, so a caller reporting "verified"
	// can say what that covered. Claiming more than was checked is how a backup
	// comes to be trusted right up to the moment it is needed.
	Checks []string

	// FilesListed is how many files the snapshot's tree resolved to, when a
	// snapshot was verified. A snapshot that lists nothing is reported rather
	// than passed, since an empty backup is almost never what was intended.
	FilesListed int64

	// BytesInSnapshot is what restoring this snapshot would produce. Comparing
	// it against what was handed to the engine is how a backup that quietly
	// captured less than it was given gets noticed here rather than at restore.
	BytesInSnapshot int64
}

// RetentionPolicy expresses which snapshots to keep. Zero values mean the
// corresponding rule is not applied.
type RetentionPolicy struct {
	KeepLast    int
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	KeepYearly  int

	// KeepWithin keeps every snapshot newer than this duration.
	KeepWithin time.Duration

	// KeepTags keeps every snapshot carrying any of these tags, regardless of
	// the counting rules above.
	KeepTags []string

	// OnlyTags restricts the policy to snapshots carrying these tags. Retention
	// in Back-Orbit is owned by a backup plan, so a plan must only ever be able
	// to delete its own snapshots — never another plan's that happen to share
	// the repository.
	OnlyTags []string

	// GroupBy controls how snapshots are bucketed before the keep rules are
	// counted, as a comma-separated list of "host", "paths" and "tags". The
	// zero value means a single group, so "keep the last 7" keeps seven
	// snapshots in total.
	//
	// This is always sent explicitly. restic's own default groups by
	// host,paths, which silently turns "keep the last 7" into "keep 7 per
	// distinct path set" — so changing which paths a plan backs up would
	// strand the previous snapshots in a group of their own that the policy
	// never prunes, growing the repository forever while the UI reports that
	// retention is applied.
	GroupBy string
}

// IsZero reports whether the policy would delete nothing because no rule is
// set. Applying such a policy is refused rather than silently pruning
// everything.
func (p RetentionPolicy) IsZero() bool {
	return p.KeepLast == 0 && p.KeepHourly == 0 && p.KeepDaily == 0 &&
		p.KeepWeekly == 0 && p.KeepMonthly == 0 && p.KeepYearly == 0 &&
		p.KeepWithin == 0
}

// BackupEngine is the contract every backup implementation must satisfy.
// Every method takes a context so long-running operations stay cancellable,
// which the job system relies on to support aborting a running backup.
type BackupEngine interface {
	InitRepository(ctx context.Context, config RepositoryConfig) error
	CreateSnapshot(ctx context.Context, request SnapshotRequest) (*SnapshotResult, error)
	ListSnapshots(ctx context.Context, repository RepositoryConfig) ([]Snapshot, error)
	RestoreSnapshot(ctx context.Context, request RestoreRequest) (*RestoreResult, error)
	VerifyRepository(ctx context.Context, repository RepositoryConfig) (*VerificationResult, error)

	// VerifySnapshot checks that one specific snapshot is actually there and
	// readable, which is not the same question VerifyRepository answers.
	//
	// A structurally sound repository can still be missing the snapshot that
	// was just written, or hold one whose tree does not resolve. Until that is
	// confirmed, a backup has only been *reported* as taken — and the one
	// moment it will be tested otherwise is the moment it is needed.
	VerifySnapshot(ctx context.Context, repository RepositoryConfig, snapshotID string) (*VerificationResult, error)
	ApplyRetention(ctx context.Context, repository RepositoryConfig, policy RetentionPolicy) error
	PruneRepository(ctx context.Context, repository RepositoryConfig) error
}
