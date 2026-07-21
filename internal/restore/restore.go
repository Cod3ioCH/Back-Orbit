// Package restore plans and supervises restores. It deliberately separates a
// non-destructive extraction from operations that mutate a running project.
package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
)

var (
	ErrNotFound       = errors.New("restore: not found")
	ErrUnsupported    = errors.New("restore: requested mode is not safe for this snapshot")
	ErrAlreadyRunning = errors.New("restore: this snapshot already has a restore running")
)

type Mode string

const (
	ModeExtract Mode = "extract"
	ModeInPlace Mode = "in_place"
	ModeClone   Mode = "clone"
)

type PreviewRequest struct {
	SnapshotID     string `json:"snapshotId"`
	Mode           Mode   `json:"mode"`
	NewProjectName string `json:"newProjectName,omitempty"`
}
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Item struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	SourcePath     string `json:"sourcePath"`
	IntendedTarget string `json:"intendedTarget"`
	Files          int64  `json:"files"`
	Bytes          int64  `json:"bytes"`
}
type Preview struct {
	SnapshotID     string  `json:"snapshotId"`
	ProjectName    string  `json:"projectName"`
	Mode           Mode    `json:"mode"`
	Supported      bool    `json:"supported"`
	Destructive    bool    `json:"destructive"`
	EstimatedBytes int64   `json:"estimatedBytes"`
	Files          int64   `json:"files"`
	Items          []Item  `json:"items"`
	Warnings       []Issue `json:"warnings"`
	Blockers       []Issue `json:"blockers"`
}

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Run struct {
	ID            string     `json:"id"`
	SnapshotID    string     `json:"snapshotId"`
	ProjectName   string     `json:"projectName"`
	Mode          Mode       `json:"mode"`
	Status        Status     `json:"status"`
	TargetPath    string     `json:"targetPath,omitempty"`
	FilesRestored int64      `json:"filesRestored"`
	BytesRestored int64      `json:"bytesRestored"`
	Warnings      []string   `json:"warnings"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}
type StartRequest struct {
	PreviewRequest
	ActorUserID string
}

type snapshotProvider interface {
	GetSnapshot(context.Context, string) (*backuprun.Snapshot, error)
}
type repositoryProvider interface {
	EngineConfig(context.Context, string) (backup.RepositoryConfig, error)
}

type Runner struct {
	db           *sql.DB
	snapshots    snapshotProvider
	repositories repositoryProvider
	engine       backup.BackupEngine
	recorder     *events.Recorder
	root         string
	// docker is needed to replay an export into the database it came from.
	// Optional: without it the file-extraction path still works.
	docker     docker.Client
	mu         sync.Mutex
	active     map[string]context.CancelFunc
	bySnapshot map[string]string
}

func NewRunner(db *sql.DB, snapshots snapshotProvider, repos repositoryProvider, engine backup.BackupEngine, recorder *events.Recorder, root string, dockerClient docker.Client) *Runner {
	return &Runner{db: db, snapshots: snapshots, repositories: repos, engine: engine, recorder: recorder, root: root, docker: dockerClient, active: map[string]context.CancelFunc{}, bySnapshot: map[string]string{}}
}

func (r *Runner) Preview(ctx context.Context, req PreviewRequest) (Preview, error) {
	s, err := r.snapshots.GetSnapshot(ctx, req.SnapshotID)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{SnapshotID: s.ID, ProjectName: s.Manifest.Project, Mode: req.Mode, Files: s.FilesCount, EstimatedBytes: s.SizeBytes, Items: []Item{}, Warnings: []Issue{}, Blockers: []Issue{}}
	if p.Mode == "" {
		p.Mode = ModeExtract
	}
	for _, v := range s.Manifest.Volumes {
		target := v.Name
		if v.Kind == "bind" {
			target = v.Name
		}
		p.Items = append(p.Items, Item{Kind: v.Kind, Name: v.Name, SourcePath: v.PathInSnapshot, IntendedTarget: target, Files: v.Files, Bytes: v.Bytes})
	}
	switch p.Mode {
	case ModeExtract:
		p.Supported = true
		p.Warnings = append(p.Warnings, Issue{Code: "isolated_extraction", Message: "Data is restored into a new protected directory. Containers and live application data are not changed."})
	case ModeInPlace:
		p.Destructive = true
		p.Blockers = append(p.Blockers, Issue{Code: "project_bundle_required", Message: "This snapshot predates the complete project bundle needed to stop services, map targets, restore ownership, and verify startup safely."})
	case ModeClone:
		p.Destructive = true
		if strings.TrimSpace(req.NewProjectName) == "" {
			p.Blockers = append(p.Blockers, Issue{Code: "project_name_required", Message: "A new Compose project name is required."})
		}
		p.Blockers = append(p.Blockers, Issue{Code: "project_bundle_required", Message: "Deploy as new requires Compose files plus explicit port, volume, network, path, image, and secret mappings. This snapshot contains data volumes only."})
	default:
		return Preview{}, fmt.Errorf("%w: unknown restore mode", ErrUnsupported)
	}
	p.Supported = p.Supported && len(p.Blockers) == 0
	return p, nil
}

func (r *Runner) Start(ctx context.Context, req StartRequest) (Run, error) {
	p, err := r.Preview(ctx, req.PreviewRequest)
	if err != nil {
		return Run{}, err
	}
	if !p.Supported || p.Mode != ModeExtract {
		return Run{}, ErrUnsupported
	}
	s, err := r.snapshots.GetSnapshot(ctx, req.SnapshotID)
	if err != nil {
		return Run{}, err
	}
	cfg, err := r.repositories.EngineConfig(ctx, s.RepositoryID)
	if err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), SnapshotID: s.ID, ProjectName: s.Manifest.Project, Mode: p.Mode, Status: StatusRunning, Warnings: []string{}, StartedAt: now, CreatedAt: now}
	run.TargetPath = filepath.Join(r.root, run.ID)
	r.mu.Lock()
	if existing, ok := r.bySnapshot[s.ID]; ok {
		r.mu.Unlock()
		return Run{}, fmt.Errorf("%w (%s)", ErrAlreadyRunning, existing)
	}
	r.bySnapshot[s.ID] = run.ID
	r.mu.Unlock()
	if err := os.MkdirAll(run.TargetPath, 0700); err != nil {
		r.release(s.ID, run.ID)
		return Run{}, fmt.Errorf("restore: create target: %w", err)
	}
	if err := r.insert(ctx, run); err != nil {
		r.release(s.ID, run.ID)
		return Run{}, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.mu.Lock()
	r.active[run.ID] = cancel
	r.mu.Unlock()
	r.recorder.Emit(ctx, events.Event{Action: events.ActionRestoreStarted, ActorUserID: req.ActorUserID, TargetType: "restore_run", TargetID: run.ID, Metadata: map[string]any{"snapshotId": s.ID, "mode": p.Mode}})
	go func() {
		defer cancel()
		defer r.release(s.ID, run.ID)
		result, e := r.engine.RestoreSnapshot(runCtx, backup.RestoreRequest{Repository: cfg, SnapshotID: s.ResticSnapshotID, TargetPath: run.TargetPath})
		end := time.Now().UTC()
		run.EndedAt = &end
		if e != nil {
			if errors.Is(e, context.Canceled) || errors.Is(runCtx.Err(), context.Canceled) {
				run.Status = StatusCancelled
			} else {
				run.Status = StatusFailed
			}
			run.Error = e.Error()
		} else {
			run.Status = StatusCompleted
			run.FilesRestored = result.FilesRestored
			run.BytesRestored = result.BytesRestored
			run.Warnings = result.Warnings
		}
		_ = r.update(context.Background(), run)
		action := events.ActionRestoreCompleted
		if run.Status == StatusFailed {
			action = events.ActionRestoreFailed
		} else if run.Status == StatusCancelled {
			action = events.ActionRestoreCancelled
		}
		r.recorder.Emit(context.Background(), events.Event{Action: action, ActorUserID: req.ActorUserID, TargetType: "restore_run", TargetID: run.ID, Metadata: map[string]any{"snapshotId": s.ID, "status": run.Status}})
	}()
	return run, nil
}

func (r *Runner) Cancel(id string) error {
	r.mu.Lock()
	c, ok := r.active[id]
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	c()
	return nil
}
func (r *Runner) release(snapshotID, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, id)
	if r.bySnapshot[snapshotID] == id {
		delete(r.bySnapshot, snapshotID)
	}
}
func (r *Runner) Get(ctx context.Context, id string) (Run, error) {
	return r.read(ctx, `WHERE id = ?`, id)
}
func (r *Runner) List(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, e := r.db.QueryContext(ctx, `SELECT `+runColumns+` FROM restore_runs ORDER BY created_at DESC LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		v, e := scanRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Runner) CloseInterruptedRuns(ctx context.Context) (int64, error) {
	res, e := r.db.ExecContext(ctx, `UPDATE restore_runs SET status='failed', error='Back-Orbit stopped while this restore was running.', ended_at=? WHERE status='running'`, time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		return 0, e
	}
	return res.RowsAffected()
}

const runColumns = `id,snapshot_id,project_name,mode,status,target_path,files_restored,bytes_restored,warnings_json,error,started_at,ended_at,created_at`

func (r *Runner) insert(ctx context.Context, v Run) error {
	w, _ := json.Marshal(v.Warnings)
	_, e := r.db.ExecContext(ctx, `INSERT INTO restore_runs (`+runColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.SnapshotID, v.ProjectName, v.Mode, v.Status, v.TargetPath, v.FilesRestored, v.BytesRestored, string(w), v.Error, v.StartedAt.Format(time.RFC3339Nano), nil, v.CreatedAt.Format(time.RFC3339Nano))
	return e
}
func (r *Runner) update(ctx context.Context, v Run) error {
	w, _ := json.Marshal(v.Warnings)
	var end any
	if v.EndedAt != nil {
		end = v.EndedAt.Format(time.RFC3339Nano)
	}
	_, e := r.db.ExecContext(ctx, `UPDATE restore_runs SET status=?,files_restored=?,bytes_restored=?,warnings_json=?,error=?,ended_at=? WHERE id=?`, v.Status, v.FilesRestored, v.BytesRestored, string(w), v.Error, end, v.ID)
	return e
}
func (r *Runner) read(ctx context.Context, where string, arg any) (Run, error) {
	v, e := scanRun(r.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM restore_runs `+where, arg))
	if errors.Is(e, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	return v, e
}

type scanner interface{ Scan(...any) error }

func scanRun(s scanner) (Run, error) {
	var v Run
	var mode, status, start, created, w string
	var end sql.NullString
	e := s.Scan(&v.ID, &v.SnapshotID, &v.ProjectName, &mode, &status, &v.TargetPath, &v.FilesRestored, &v.BytesRestored, &w, &v.Error, &start, &end, &created)
	if e != nil {
		return v, e
	}
	v.Mode = Mode(mode)
	v.Status = Status(status)
	_ = json.Unmarshal([]byte(w), &v.Warnings)
	v.StartedAt, _ = time.Parse(time.RFC3339Nano, start)
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if end.Valid {
		t, _ := time.Parse(time.RFC3339Nano, end.String)
		v.EndedAt = &t
	}
	return v, nil
}
