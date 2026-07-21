package backuprun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a run or snapshot does not exist.
var ErrNotFound = errors.New("backuprun: not found")

type store struct{ db *sql.DB }

func newStore(db *sql.DB) *store { return &store{db: db} }

func (s *store) insertRun(ctx context.Context, run Run) error {
	volumes, err := json.Marshal(run.Volumes)
	if err != nil {
		return fmt.Errorf("backuprun: encode volumes: %w", err)
	}
	warnings, err := json.Marshal(emptyIfNil(run.Warnings))
	if err != nil {
		return fmt.Errorf("backuprun: encode warnings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO backup_runs (
			id, project_id, project_name, repository_id, repository_name,
			trigger, status, phase, volumes_json,
			files_total, bytes_total, bytes_added,
			warnings_json, error, started_at, ended_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, nullable(run.ProjectID), run.ProjectName, nullable(run.RepositoryID), run.RepositoryName,
		string(run.Trigger), string(run.Status), string(run.Phase), string(volumes),
		run.FilesTotal, run.BytesTotal, run.BytesAdded,
		string(warnings), run.Error, formatTime(run.StartedAt), nullableTime(run.EndedAt), formatTime(run.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("backuprun: insert run: %w", err)
	}
	return nil
}

func (s *store) updateRun(ctx context.Context, run Run) error {
	warnings, err := json.Marshal(emptyIfNil(run.Warnings))
	if err != nil {
		return fmt.Errorf("backuprun: encode warnings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE backup_runs SET
			status = ?, phase = ?,
			files_total = ?, bytes_total = ?, bytes_added = ?,
			warnings_json = ?, error = ?, ended_at = ?
		WHERE id = ?`,
		string(run.Status), string(run.Phase),
		run.FilesTotal, run.BytesTotal, run.BytesAdded,
		string(warnings), run.Error, nullableTime(run.EndedAt), run.ID,
	)
	if err != nil {
		return fmt.Errorf("backuprun: update run: %w", err)
	}
	return nil
}

func (s *store) insertSnapshot(ctx context.Context, snapshot Snapshot) error {
	manifest, err := json.Marshal(snapshot.Manifest)
	if err != nil {
		return fmt.Errorf("backuprun: encode manifest: %w", err)
	}
	verification, err := json.Marshal(snapshot.Verification)
	if err != nil {
		return fmt.Errorf("backuprun: encode verification: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO snapshots (
			id, run_id, repository_id, restic_snapshot_id, manifest_json,
			size_bytes, files_count, verified_at, verification_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ID, snapshot.RunID, nullable(snapshot.RepositoryID), snapshot.ResticSnapshotID, string(manifest),
		snapshot.SizeBytes, snapshot.FilesCount, nullableTime(snapshot.VerifiedAt),
		string(verification), formatTime(snapshot.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("backuprun: insert snapshot: %w", err)
	}
	return nil
}

const runColumns = `id, project_id, project_name, repository_id, repository_name,
	trigger, status, phase, volumes_json, files_total, bytes_total, bytes_added,
	warnings_json, error, started_at, ended_at, created_at`

func (s *store) getRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM backup_runs WHERE id = ?`, id)
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}

	snapshot, err := s.snapshotForRun(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Run{}, err
	}
	run.Snapshot = snapshot
	return run, nil
}

func (s *store) listRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM backup_runs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("backuprun: list runs: %w", err)
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backuprun: list runs: %w", err)
	}

	// Attaching the snapshot per run keeps the list honest about which runs
	// actually left a verified backup behind — the only thing that separates a
	// green row from a useful one.
	for i := range runs {
		snapshot, err := s.snapshotForRun(ctx, runs[i].ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		runs[i].Snapshot = snapshot
	}
	return runs, nil
}

func (s *store) snapshotForRun(ctx context.Context, runID string) (*Snapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, run_id, repository_id, restic_snapshot_id, manifest_json,
		       size_bytes, files_count, verified_at, verification_json, created_at
		FROM snapshots WHERE run_id = ?`, runID)

	var (
		snapshot                       Snapshot
		repositoryID                   sql.NullString
		manifestJSON, verificationJSON string
		verifiedAt                     sql.NullString
		createdAt                      string
	)
	err := row.Scan(&snapshot.ID, &snapshot.RunID, &repositoryID, &snapshot.ResticSnapshotID,
		&manifestJSON, &snapshot.SizeBytes, &snapshot.FilesCount, &verifiedAt,
		&verificationJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("backuprun: read snapshot: %w", err)
	}

	snapshot.RepositoryID = repositoryID.String
	if err := json.Unmarshal([]byte(manifestJSON), &snapshot.Manifest); err != nil {
		return nil, fmt.Errorf("backuprun: decode manifest: %w", err)
	}
	if err := json.Unmarshal([]byte(verificationJSON), &snapshot.Verification); err != nil {
		return nil, fmt.Errorf("backuprun: decode verification: %w", err)
	}
	snapshot.CreatedAt = parseTime(createdAt)
	if verifiedAt.Valid {
		at := parseTime(verifiedAt.String)
		snapshot.VerifiedAt = &at
	}
	return &snapshot, nil
}

// markInterruptedRuns closes out runs that were still marked running when the
// process stopped.
//
// A backup runs in memory: if Back-Orbit is killed mid-run, nothing is left to
// finish or fail it. Left alone the row would claim "running" forever, which
// reads as a backup still in progress rather than one that never completed —
// the most misleading state a backup tool can display.
func (s *store) markInterruptedRuns(ctx context.Context) (int64, error) {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `
		UPDATE backup_runs
		SET status = ?, phase = ?, ended_at = ?,
		    error = 'Back-Orbit stopped while this backup was running, so it did not finish. No verified snapshot was recorded.'
		WHERE status = ?`,
		string(StatusFailed), string(PhaseFinished), now, string(StatusRunning))
	if err != nil {
		return 0, fmt.Errorf("backuprun: close interrupted runs: %w", err)
	}
	return result.RowsAffected()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(row scanner) (Run, error) {
	var (
		run                       Run
		projectID, repositoryID   sql.NullString
		volumesJSON, warningsJSON string
		trigger, status, phase    string
		startedAt, createdAt      string
		endedAt                   sql.NullString
	)
	err := row.Scan(&run.ID, &projectID, &run.ProjectName, &repositoryID, &run.RepositoryName,
		&trigger, &status, &phase, &volumesJSON,
		&run.FilesTotal, &run.BytesTotal, &run.BytesAdded,
		&warningsJSON, &run.Error, &startedAt, &endedAt, &createdAt)
	if err != nil {
		return Run{}, err
	}

	run.ProjectID = projectID.String
	run.RepositoryID = repositoryID.String
	run.Trigger = Trigger(trigger)
	run.Status = Status(status)
	run.Phase = Phase(phase)
	if err := json.Unmarshal([]byte(volumesJSON), &run.Volumes); err != nil {
		return Run{}, fmt.Errorf("backuprun: decode volumes: %w", err)
	}
	if err := json.Unmarshal([]byte(warningsJSON), &run.Warnings); err != nil {
		return Run{}, fmt.Errorf("backuprun: decode warnings: %w", err)
	}
	run.StartedAt = parseTime(startedAt)
	run.CreatedAt = parseTime(createdAt)
	if endedAt.Valid {
		at := parseTime(endedAt.String)
		run.EndedAt = &at
	}
	return run, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
