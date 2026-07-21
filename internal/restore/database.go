package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/dbdump"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
)

// ModeDatabase replays one database's export back into the running service it
// came from.
const ModeDatabase Mode = "database"

var (
	// ErrNotConfirmed is returned when the request did not name its target.
	ErrNotConfirmed = errors.New("restore: this replaces a live database and must be confirmed by naming the service")
	// ErrNoExport is returned when the snapshot holds no export for that
	// database — only the files underneath, which this path cannot replay.
	ErrNoExport = errors.New("restore: this snapshot holds no export for that database")
)

// DatabaseRequest asks for one database to be restored from a snapshot.
type DatabaseRequest struct {
	SnapshotID string
	Service    string

	// Confirm must equal Service.
	//
	// Enforced here rather than only in the UI. This call replaces a live
	// database, and a confirmation a client can skip by calling the API
	// directly is not a confirmation — it is a dialog. Naming the target is
	// something no accidental request does.
	Confirm string

	ActorUserID string
}

// RestoreDatabase replays a database export from a snapshot into the running
// service it was taken from.
//
// The whole operation is destructive by design: the export is loaded with the
// engine's own "drop and recreate" semantics, because a restore that merges
// into whatever is already there produces a state that never existed.
func (r *Runner) RestoreDatabase(ctx context.Context, req DatabaseRequest) (Run, error) {
	if req.Confirm == "" || req.Confirm != req.Service {
		return Run{}, ErrNotConfirmed
	}
	if r.docker == nil {
		return Run{}, errors.New("restore: Docker is unavailable, so no database can be restored")
	}

	snapshot, err := r.snapshots.GetSnapshot(ctx, req.SnapshotID)
	if err != nil {
		return Run{}, err
	}

	export, found := findExport(snapshot, req.Service)
	if !found {
		return Run{}, fmt.Errorf("%w: %s", ErrNoExport, req.Service)
	}

	container, err := r.findServiceContainer(ctx, snapshot.Manifest.Project, req.Service)
	if err != nil {
		return Run{}, err
	}

	config, err := r.repositories.EngineConfig(ctx, snapshot.RepositoryID)
	if err != nil {
		return Run{}, err
	}

	now := time.Now().UTC()
	run := Run{
		ID: uuid.NewString(), SnapshotID: snapshot.ID, ProjectName: snapshot.Manifest.Project,
		Mode: ModeDatabase, Status: StatusRunning, Warnings: []string{},
		StartedAt: now, CreatedAt: now,
	}
	run.TargetPath = filepath.Join(r.root, run.ID)

	r.mu.Lock()
	if existing, busy := r.bySnapshot[snapshot.ID]; busy {
		r.mu.Unlock()
		return Run{}, fmt.Errorf("%w (%s)", ErrAlreadyRunning, existing)
	}
	r.bySnapshot[snapshot.ID] = run.ID
	r.mu.Unlock()

	if err := os.MkdirAll(run.TargetPath, 0o700); err != nil {
		r.release(snapshot.ID, run.ID)
		return Run{}, fmt.Errorf("restore: create working directory: %w", err)
	}
	if err := r.insert(ctx, run); err != nil {
		r.release(snapshot.ID, run.ID)
		return Run{}, err
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.mu.Lock()
	r.active[run.ID] = cancel
	r.mu.Unlock()

	r.recorder.Emit(ctx, events.Event{
		Action: events.ActionRestoreStarted, ActorUserID: req.ActorUserID,
		TargetType: "restore_run", TargetID: run.ID,
		Metadata: map[string]any{
			"snapshotId": snapshot.ID, "mode": ModeDatabase,
			"service": req.Service, "technology": export.Technology,
		},
	})

	go func() {
		defer cancel()
		defer r.release(snapshot.ID, run.ID)
		defer os.RemoveAll(run.TargetPath)

		r.runDatabaseRestore(runCtx, &run, snapshot, export, container, config, req.ActorUserID)
	}()

	return run, nil
}

func (r *Runner) runDatabaseRestore(
	ctx context.Context,
	run *Run,
	snapshot *backuprun.Snapshot,
	export backuprun.DatabaseDump,
	container docker.Container,
	config backup.RepositoryConfig,
	actorUserID string,
) {
	// Counted after the load where the engine can be asked, so the activity
	// feed records what came back rather than only that something ran.
	objects := 0

	finish := func(status Status, err error) {
		ended := time.Now().UTC()
		run.EndedAt = &ended
		run.Status = status
		if err != nil {
			run.Error = err.Error()
		}
		_ = r.update(context.WithoutCancel(ctx), *run)

		action := events.ActionRestoreCompleted
		switch status {
		case StatusFailed:
			action = events.ActionRestoreFailed
		case StatusCancelled:
			action = events.ActionRestoreCancelled
		}
		r.recorder.Emit(context.WithoutCancel(ctx), events.Event{
			Action: action, ActorUserID: actorUserID,
			TargetType: "restore_run", TargetID: run.ID,
			Metadata: map[string]any{
				"snapshotId": snapshot.ID, "status": status,
				"service": export.Service, "technology": export.Technology,
				"objects": objects,
			},
		})
	}

	// Only the export is pulled out of the snapshot, not the volume copies
	// beside it. Restoring a database means replaying its export; dragging the
	// raw data directory along would fight with the server that is running on
	// it right now.
	if _, err := r.engine.RestoreSnapshot(ctx, backup.RestoreRequest{
		Repository: config,
		SnapshotID: snapshot.ResticSnapshotID,
		TargetPath: run.TargetPath,
		Include:    []string{"*/" + filepath.Base(export.Path)},
	}); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			finish(StatusCancelled, errors.New("cancelled before the database was touched"))
			return
		}
		finish(StatusFailed, fmt.Errorf("extract the export from the snapshot: %w", err))
		return
	}

	dumpPath, err := findExtracted(run.TargetPath, filepath.Base(export.Path))
	if err != nil {
		finish(StatusFailed, err)
		return
	}

	user, password := r.credentialsFor(ctx, container.ID, export.Technology)
	result, err := dbdump.Load(ctx, r.docker, dbdump.Target{
		Technology:  export.Technology,
		Service:     export.Service,
		ContainerID: container.ID,
		User:        user,
		Password:    password,
	}, dumpPath)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			// The load may have applied part of the export before being cut
			// off, so this cannot claim the database is untouched.
			finish(StatusCancelled, errors.New(
				"cancelled during the load; the database may hold a partial restore"))
			return
		}
		finish(StatusFailed, err)
		return
	}

	run.BytesRestored = result.Bytes
	objects = result.Objects
	if result.Output != "" {
		run.Warnings = append(run.Warnings, result.Output)
	}
	finish(StatusCompleted, nil)
}

// findExport returns the export for a service, if the snapshot carries one.
func findExport(snapshot *backuprun.Snapshot, service string) (backuprun.DatabaseDump, bool) {
	for _, database := range snapshot.Manifest.Databases {
		if database.Service != service {
			continue
		}
		// Only an export can be replayed. A database that was merely copied as
		// files has no command that puts it back, and offering to "restore" it
		// would promise something this path cannot do.
		if database.Level == backuprun.ProtectionExported && database.Path != "" {
			return database, true
		}
	}
	return backuprun.DatabaseDump{}, false
}

// findExtracted locates the export inside the restored tree, whose directory
// layout mirrors the absolute path the backup was taken from.
func findExtracted(root, name string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("restore: search the extracted snapshot: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("restore: %s was not in the snapshot where the manifest said it would be", name)
	}
	return found, nil
}

// findServiceContainer locates the running container for a Compose service.
func (r *Runner) findServiceContainer(ctx context.Context, project, service string) (docker.Container, error) {
	live, err := r.docker.GetComposeProject(ctx, project)
	if err != nil {
		return docker.Container{}, fmt.Errorf("restore: find project %q: %w", project, err)
	}
	for _, container := range live.Containers {
		if container.Service == service {
			return container, nil
		}
	}
	return docker.Container{}, fmt.Errorf(
		"restore: service %q is not running in project %q, so there is nothing to restore into",
		service, project)
}

// credentialsFor reads the one or two values a load needs from the target
// container's own environment. Only these keys are ever read.
func (r *Runner) credentialsFor(ctx context.Context, containerID, technology string) (user, password string) {
	read := func(keys ...string) string {
		for _, key := range keys {
			if value, err := r.docker.ContainerEnvValue(ctx, containerID, key); err == nil && value != "" {
				return value
			}
		}
		return ""
	}

	switch technology {
	case "postgresql":
		// No password: the load runs inside the container over its local
		// socket, where the server trusts its own operating-system user.
		return read("POSTGRES_USER"), ""
	case "mysql":
		return read("MYSQL_ROOT_USER"), read("MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD")
	case "mariadb":
		return read("MARIADB_ROOT_USER"), read("MARIADB_ROOT_PASSWORD", "MYSQL_ROOT_PASSWORD")
	case "mongodb":
		return read("MONGO_INITDB_ROOT_USERNAME"), read("MONGO_INITDB_ROOT_PASSWORD")
	default:
		return "", ""
	}
}
