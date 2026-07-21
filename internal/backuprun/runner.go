package backuprun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
	"github.com/Cod3ioCH/Back-Orbit/internal/repositories"
	"github.com/Cod3ioCH/Back-Orbit/internal/storage"
)

// ErrNothingToBackUp is returned when a project has no named volumes.
var ErrNothingToBackUp = errors.New("backuprun: this project has no named volumes to back up")

// ErrAlreadyRunning is returned when a project already has a backup in flight.
var ErrAlreadyRunning = errors.New("backuprun: a backup for this project is already running")

// Runner starts and supervises backup runs.
type Runner struct {
	store      *store
	projects   *projects.Service
	repos      *repositories.Service
	stager     *storage.Stager
	engine     backup.BackupEngine
	recorder   *events.Recorder
	stagingDir string

	// mu guards active. A run is cancellable only while this process is the
	// one running it, which is why cancellation lives in memory and
	// interrupted runs are closed out at startup instead.
	mu     sync.Mutex
	active map[string]context.CancelFunc
	// byProject stops the same project being backed up twice at once, which
	// would have two runs staging the same volumes into different directories
	// and racing for the repository lock.
	byProject map[string]string
}

// NewRunner wires up a Runner. stagingDir is where volume contents are
// materialised while a backup runs.
func NewRunner(
	db *sql.DB,
	projectService *projects.Service,
	repoService *repositories.Service,
	stager *storage.Stager,
	engine backup.BackupEngine,
	recorder *events.Recorder,
	stagingDir string,
) *Runner {
	return &Runner{
		store:      newStore(db),
		projects:   projectService,
		repos:      repoService,
		stager:     stager,
		engine:     engine,
		recorder:   recorder,
		stagingDir: stagingDir,
		active:     map[string]context.CancelFunc{},
		byProject:  map[string]string{},
	}
}

// StartRequest describes a backup to run now.
type StartRequest struct {
	ProjectID    string
	RepositoryID string
	ActorUserID  string
	Trigger      Trigger
}

// Start begins a backup and returns as soon as it is under way.
//
// The work continues on its own goroutine with a context detached from the
// caller's: a backup that stopped because the browser tab closed would be the
// worst kind of unreliable. Cancelling is an explicit act, through Cancel.
func (r *Runner) Start(ctx context.Context, req StartRequest) (Run, error) {
	project, err := r.projects.Get(ctx, req.ProjectID)
	if err != nil {
		return Run{}, err
	}
	repository, err := r.repos.Get(ctx, req.RepositoryID)
	if err != nil {
		return Run{}, err
	}

	// Resolved before the run is recorded, so a locked secret store fails the
	// request rather than producing a run that dies a moment later.
	engineConfig, err := r.repos.EngineConfig(ctx, req.RepositoryID)
	if err != nil {
		return Run{}, err
	}

	// Named volumes and bind mounts alike. Skipped sources (the Docker socket,
	// host system files) are left out here rather than failing the run: they
	// are listed in the UI with the reason, so nothing disappears silently.
	sources := make([]projects.BackupSource, 0, len(project.Sources))
	volumes := make([]string, 0, len(project.Sources))
	for _, source := range project.Sources {
		if !source.Backupable() {
			continue
		}
		sources = append(sources, source)
		volumes = append(volumes, source.Name)
	}
	if len(sources) == 0 {
		if !project.DockerAvailable {
			return Run{}, fmt.Errorf("%w: Docker is unreachable, so its data could not be listed",
				ErrNothingToBackUp)
		}
		return Run{}, ErrNothingToBackUp
	}

	trigger := req.Trigger
	if trigger == "" {
		trigger = TriggerManual
	}

	now := time.Now().UTC()
	run := Run{
		ID:             uuid.NewString(),
		ProjectID:      project.ID,
		ProjectName:    project.Name,
		RepositoryID:   repository.ID,
		RepositoryName: repository.Name,
		Trigger:        trigger,
		Status:         StatusRunning,
		Phase:          PhasePreparing,
		Volumes:        volumes,
		sources:        sources,
		Warnings:       []string{},
		StartedAt:      now,
		CreatedAt:      now,
	}

	r.mu.Lock()
	if existing, busy := r.byProject[project.ID]; busy {
		r.mu.Unlock()
		return Run{}, fmt.Errorf("%w (run %s)", ErrAlreadyRunning, existing)
	}
	// Reserved before the insert so two simultaneous requests cannot both get
	// past the check.
	r.byProject[project.ID] = run.ID
	r.mu.Unlock()

	if err := r.store.insertRun(ctx, run); err != nil {
		r.release(project.ID, run.ID)
		return Run{}, err
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.mu.Lock()
	r.active[run.ID] = cancel
	r.mu.Unlock()

	r.recorder.Emit(ctx, events.Event{
		Action:      events.ActionBackupStarted,
		ActorUserID: req.ActorUserID,
		TargetType:  "backup_run",
		TargetID:    run.ID,
		Metadata: map[string]any{
			"project": project.Name, "repository": repository.Name, "volumes": len(volumes),
		},
	})

	go func() {
		defer cancel()
		defer r.release(project.ID, run.ID)
		r.execute(runCtx, run, engineConfig, req.ActorUserID)
	}()

	return run, nil
}

// Cancel stops a running backup.
func (r *Runner) Cancel(id string) error {
	r.mu.Lock()
	cancel, ok := r.active[id]
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	cancel()
	return nil
}

// Get returns one run, including its verified snapshot when it produced one.
func (r *Runner) Get(ctx context.Context, id string) (Run, error) {
	return r.store.getRun(ctx, id)
}

// List returns recent runs, newest first.
func (r *Runner) List(ctx context.Context, limit int) ([]Run, error) {
	return r.store.listRuns(ctx, limit)
}

// CloseInterruptedRuns fails any run left marked running by a previous
// process. Called at startup.
func (r *Runner) CloseInterruptedRuns(ctx context.Context) (int64, error) {
	return r.store.markInterruptedRuns(ctx)
}

func (r *Runner) release(projectID, runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, runID)
	if r.byProject[projectID] == runID {
		delete(r.byProject, projectID)
	}
}

// execute performs the run. It never returns an error: every outcome is
// recorded on the run itself, because by this point there is no caller left to
// return one to.
func (r *Runner) execute(ctx context.Context, run Run, config backup.RepositoryConfig, actorUserID string) {
	// The staging path is derived from the project, never from the run, and
	// this matters more than it looks. restic records the absolute path it
	// backed up, so a per-run directory would file every backup of the same
	// volume under a different path: `restic diff` between two runs becomes
	// meaningless, browsing a snapshot shows a UUID, and retention grouped by
	// path puts every run in a group of its own that the policy never prunes —
	// the repository then grows forever while the UI reports retention as
	// applied. A stable path keeps successive backups comparable.
	workDir := filepath.Join(r.stagingDir, pathSegment(run.ProjectName))

	// Cleared first: because the path is stable, anything left by a run that
	// died before its own cleanup is sitting exactly where this one is about
	// to stage, and would silently be backed up as though it were current.
	if err := os.RemoveAll(workDir); err != nil {
		r.finishFailed(ctx, &run, fmt.Errorf("clearing the staging directory: %w", err), actorUserID)
		return
	}

	// Staged volume contents are a full copy of the data, so leaving them
	// behind would fill the disk one backup at a time. This runs on every exit,
	// including cancellation.
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			slog.Error("backuprun: could not remove staging directory; it will keep using disk space",
				"run", run.ID, "dir", workDir, "error", err)
		}
	}()

	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Project:       run.ProjectName,
		CreatedAt:     run.StartedAt,
	}

	// --- Staging ---------------------------------------------------------
	run.Phase = PhaseStaging
	r.persist(ctx, &run)

	stagedPaths := make([]string, 0, len(run.sources))
	for _, source := range run.sources {
		name := source.Name
		if ctx.Err() != nil {
			r.finishCancelled(ctx, &run, actorUserID)
			return
		}

		dest := filepath.Join(workDir, pathSegment(name))

		var (
			result *storage.Result
			err    error
		)
		switch source.Kind {
		case projects.SourceBind:
			result, err = r.stager.StageBindMount(ctx, source.Name, dest)
		default:
			result, err = r.stager.StageVolume(ctx, source.Name, dest)
		}
		if err != nil {
			// Cancellation surfaces here as an ordinary error from whatever
			// call was interrupted. Reporting it as a failure would tell the
			// operator something broke, when in fact they asked for this — and
			// a backup history that cannot tell those apart is one nobody can
			// use to find real problems.
			if r.wasCancelled(ctx, err) {
				r.finishCancelled(ctx, &run, actorUserID)
				return
			}
			r.finishFailed(ctx, &run, fmt.Errorf("staging volume %q: %w", name, err), actorUserID)
			return
		}

		stagedPaths = append(stagedPaths, dest)
		run.FilesTotal += int64(result.Files)
		run.BytesTotal += result.Bytes
		run.Warnings = append(run.Warnings, prefixWarnings(source, result.Warnings)...)

		manifest.Volumes = append(manifest.Volumes, VolumeManifest{
			Name:               name,
			Kind:               string(source.Kind),
			MountedAt:          source.MountedAt,
			PathInSnapshot:     dest,
			SQLiteDatabases:    result.SQLiteDatabases,
			Files:              int64(result.Files),
			Bytes:              result.Bytes,
			Ownership:          result.Ownership,
			OwnershipPreserved: result.OwnershipPreserved,
			Warnings:           result.Warnings,
		})
		r.persist(ctx, &run)
	}

	// --- Snapshot --------------------------------------------------------
	run.Phase = PhaseSnapshotting
	r.persist(ctx, &run)

	snapshotResult, err := r.engine.CreateSnapshot(ctx, backup.SnapshotRequest{
		Repository: config,
		Paths:      stagedPaths,
		// Tags are how a plan finds its own snapshots later, and how retention
		// is kept from touching another plan's.
		Tags: []string{"back-orbit", "project:" + run.ProjectName},
		// Pinned so snapshots stay attributable to the project rather than to
		// whatever hostname the container happened to have.
		Host: "back-orbit",
	})
	if err != nil {
		if r.wasCancelled(ctx, err) {
			r.finishCancelled(ctx, &run, actorUserID)
			return
		}
		r.finishFailed(ctx, &run, fmt.Errorf("creating the snapshot: %w", err), actorUserID)
		return
	}

	run.BytesAdded = snapshotResult.DataAdded
	run.Warnings = append(run.Warnings, snapshotResult.Warnings...)

	// --- Verification ----------------------------------------------------
	// The run is not finished when restic exits zero. Until the snapshot has
	// been read back it is only a claim, and a claim is what a backup tool
	// must never present as a backup.
	run.Phase = PhaseVerifying
	r.persist(ctx, &run)

	verification, err := r.engine.VerifySnapshot(ctx, config, snapshotResult.SnapshotID)
	if err != nil {
		if r.wasCancelled(ctx, err) {
			r.finishCancelled(ctx, &run, actorUserID)
			return
		}
		r.finishFailed(ctx, &run, fmt.Errorf("verifying snapshot %s: %w", snapshotResult.SnapshotID, err), actorUserID)
		return
	}
	if !verification.OK {
		r.finishFailed(ctx, &run, fmt.Errorf(
			"snapshot %s was written but could not be verified: %v",
			snapshotResult.SnapshotID, verification.Errors), actorUserID)
		return
	}

	// What the snapshot would restore is compared against what was handed to
	// the engine. This is the check that catches a backup which quietly
	// captured less than it was given — the failure that otherwise stays
	// invisible until a restore comes up short.
	//
	// Only a shortfall is reported. A snapshot can legitimately measure larger,
	// because restore size counts each hard link's content while staging
	// counted it once, and turning that into an alarm would train people to
	// ignore the one warning that matters.
	if verification.BytesInSnapshot > 0 && verification.BytesInSnapshot < run.BytesTotal {
		run.Warnings = append(run.Warnings, fmt.Sprintf(
			"the snapshot would restore %d bytes but %d were staged; %d bytes did not make it in",
			verification.BytesInSnapshot, run.BytesTotal, run.BytesTotal-verification.BytesInSnapshot))
	}

	verifiedAt := time.Now().UTC()
	snapshot := Snapshot{
		ID:               uuid.NewString(),
		RunID:            run.ID,
		RepositoryID:     run.RepositoryID,
		ResticSnapshotID: snapshotResult.SnapshotID,
		Manifest:         manifest,
		SizeBytes:        snapshotResult.TotalBytes,
		FilesCount:       snapshotResult.TotalFiles,
		VerifiedAt:       &verifiedAt,
		Verification: Verification{
			OK:              verification.OK,
			Checks:          verification.Checks,
			Errors:          verification.Errors,
			FilesListed:     verification.FilesListed,
			BytesInSnapshot: verification.BytesInSnapshot,
			DurationMS:      verification.Duration.Milliseconds(),
		},
		CreatedAt: verifiedAt,
	}

	// Written with a context detached from cancellation: the snapshot exists
	// and is verified, so losing the only record of it because someone pressed
	// cancel a moment too late would strand real data in the repository with
	// nothing pointing at it.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := r.store.insertSnapshot(persistCtx, snapshot); err != nil {
		r.finishFailed(ctx, &run, fmt.Errorf(
			"snapshot %s was created and verified, but recording it failed: %w",
			snapshotResult.SnapshotID, err), actorUserID)
		return
	}

	run.Phase = PhaseFinished
	run.Status = StatusCompleted
	if len(run.Warnings) > 0 {
		run.Status = StatusCompletedWithWarnings
	}
	ended := time.Now().UTC()
	run.EndedAt = &ended
	run.Snapshot = &snapshot
	r.persistDetached(ctx, &run)

	r.recorder.Emit(context.WithoutCancel(ctx), events.Event{
		Action:      events.ActionBackupCompleted,
		ActorUserID: actorUserID,
		TargetType:  "backup_run",
		TargetID:    run.ID,
		Metadata: map[string]any{
			"project":  run.ProjectName,
			"snapshot": snapshotResult.SnapshotID,
			"files":    run.FilesTotal,
			"warnings": len(run.Warnings),
			"verified": true,
		},
	})
}

// wasCancelled reports whether an error is the consequence of this run being
// cancelled rather than something going wrong.
//
// Both the context and the error are consulted. The context alone is not
// enough — a run can be cancelled a moment after a genuine failure — and the
// error alone is not either, since cancellation reaches callers wrapped in
// whatever operation was interrupted.
func (r *Runner) wasCancelled(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if backup.KindOf(err) == backup.ErrKindCancelled {
		return true
	}
	return ctx.Err() != nil
}

func (r *Runner) finishFailed(ctx context.Context, run *Run, err error, actorUserID string) {
	run.Status = StatusFailed
	run.Phase = PhaseFinished
	run.Error = err.Error()
	ended := time.Now().UTC()
	run.EndedAt = &ended
	r.persistDetached(ctx, run)

	r.recorder.Emit(context.WithoutCancel(ctx), events.Event{
		Action:      events.ActionBackupFailed,
		ActorUserID: actorUserID,
		TargetType:  "backup_run",
		TargetID:    run.ID,
		Metadata:    map[string]any{"project": run.ProjectName, "error": run.Error},
	})
}

func (r *Runner) finishCancelled(ctx context.Context, run *Run, actorUserID string) {
	run.Status = StatusCancelled
	run.Phase = PhaseFinished
	run.Error = "cancelled before a verified snapshot was recorded"
	ended := time.Now().UTC()
	run.EndedAt = &ended
	r.persistDetached(ctx, run)

	r.recorder.Emit(context.WithoutCancel(ctx), events.Event{
		Action:      events.ActionBackupCancelled,
		ActorUserID: actorUserID,
		TargetType:  "backup_run",
		TargetID:    run.ID,
		Metadata:    map[string]any{"project": run.ProjectName},
	})
}

// persist writes progress. A failure here loses an update, not the run, so it
// is logged rather than aborting a backup that is otherwise working.
func (r *Runner) persist(ctx context.Context, run *Run) {
	if err := r.store.updateRun(ctx, *run); err != nil {
		slog.Error("backuprun: could not record progress", "run", run.ID, "error", err)
	}
}

// persistDetached writes the final state with a context that survives
// cancellation, so the outcome is recorded even for a run that was cancelled.
func (r *Runner) persistDetached(ctx context.Context, run *Run) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := r.store.updateRun(writeCtx, *run); err != nil {
		slog.Error("backuprun: could not record the outcome of a run", "run", run.ID, "error", err)
	}
}

// pathSegment turns a project or volume name into one safe directory name.
//
// These names come from Docker labels, so they are attacker-influenceable in
// principle and must never be able to escape the staging root or reach into a
// sibling. Separators and traversal are removed rather than escaped, and an
// empty result falls back to a fixed name so a pathological input cannot
// collapse the path entirely.
func pathSegment(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	segment := strings.Trim(b.String(), ".-")
	if segment == "" {
		return "unnamed"
	}
	if len(segment) > 96 {
		segment = segment[:96]
	}
	return segment
}

func prefixWarnings(source projects.BackupSource, warnings []string) []string {
	if len(warnings) == 0 {
		return nil
	}
	prefixed := make([]string, 0, len(warnings))
	label := "named volume"
	if source.Kind == projects.SourceBind {
		label = "bind mount"
	}
	for _, warning := range warnings {
		prefixed = append(prefixed, fmt.Sprintf("%s %s: %s", label, source.Name, warning))
	}
	return prefixed
}
