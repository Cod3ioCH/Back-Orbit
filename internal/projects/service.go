package projects

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/back-orbit/back-orbit/internal/docker"
	"github.com/back-orbit/back-orbit/internal/events"
)

// knownComposeFilenames are the filenames Back-Orbit recognizes as Compose
// files when scanning a registered project directory, per the product spec.
var knownComposeFilenames = []string{
	"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml",
}

// Service is the application-level entry point for project management: it
// combines live Docker discovery, the persisted project registry, and audit
// event emission.
type Service struct {
	store    *store
	docker   docker.Client
	recorder *events.Recorder
}

// NewService creates a Service. docker may be nil, in which case Docker
// discovery is skipped and DockerAvailable is always false — this keeps the
// service usable (e.g. in tests, or when the socket briefly isn't mounted)
// without special-casing nil checks at every call site.
func NewService(db *sql.DB, dockerClient docker.Client, recorder *events.Recorder) *Service {
	return &Service{
		store:    newStore(db),
		docker:   dockerClient,
		recorder: recorder,
	}
}

// List returns all registered projects, discovered or manually registered.
func (s *Service) List(ctx context.Context) ([]Record, error) {
	return s.store.list(ctx)
}

// Get returns a single project's detail, including live Docker state when
// available. A Docker error (daemon unreachable, project not currently
// running) is surfaced as DockerWarning rather than an error return, since
// the project record itself is still valid.
func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	record, err := s.store.getByID(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	return s.attachDockerState(ctx, record), nil
}

// Scan queries the Docker daemon for running Compose projects and upserts
// each one into the project registry, then returns the full, refreshed
// list. It is safe to call when Docker is unreachable: it returns the
// existing registry unchanged along with a descriptive error the caller can
// surface as a warning.
func (s *Service) Scan(ctx context.Context, actorUserID string) ([]Record, error) {
	if s.docker == nil {
		return s.store.list(ctx)
	}

	discovered, err := s.docker.ListComposeProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan docker compose projects: %w", err)
	}

	for _, dp := range discovered {
		if _, err := s.store.upsertDiscovered(ctx, dp.Name, dp.WorkingDir, dp.ConfigFiles); err != nil {
			return nil, fmt.Errorf("register discovered project %q: %w", dp.Name, err)
		}
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionProjectScanned,
		ActorUserID: actorUserID,
		Metadata:    map[string]any{"discoveredCount": len(discovered)},
	})

	return s.store.list(ctx)
}

// Register manually adds a project by its on-disk Compose project directory.
// It is intended for projects Back-Orbit should track even when not
// currently running. The directory is scanned for recognized Compose
// filenames, but registration succeeds even if none are found or the
// directory is unreadable (recorded as an empty ComposeFiles list) —
// consistent with treating unreadable/missing paths as a warning rather
// than a hard failure (see docs/threat-model.md).
func (s *Service) Register(ctx context.Context, actorUserID, name, path string) (Record, error) {
	cleanPath, err := validateProjectPath(path)
	if err != nil {
		return Record{}, err
	}

	composeFiles := detectComposeFiles(cleanPath)

	now := time.Now().UTC()
	record := Record{
		ID:           uuid.NewString(),
		Name:         name,
		ComposePath:  cleanPath,
		ComposeFiles: composeFiles,
		Source:       SourceRegistered,
		Status:       StatusUnprotected,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.insert(ctx, record); err != nil {
		return Record{}, err
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionProjectRegistered,
		ActorUserID: actorUserID,
		TargetType:  "project",
		TargetID:    record.ID,
		Metadata:    map[string]any{"name": name, "composePath": cleanPath, "composeFilesFound": len(composeFiles)},
	})

	return record, nil
}

func (s *Service) attachDockerState(ctx context.Context, record Record) Detail {
	detail := Detail{
		Record:     record,
		Containers: []docker.Container{},
		Volumes:    []docker.Volume{},
		Networks:   []docker.Network{},
	}

	if s.docker == nil {
		detail.DockerWarning = "Docker integration is not configured."
		return detail
	}

	status := s.docker.Status(ctx)
	if !status.Connected {
		detail.DockerWarning = fmt.Sprintf("Docker daemon unreachable: %s", status.Error)
		return detail
	}

	live, err := s.docker.GetComposeProject(ctx, record.Name)
	if err != nil {
		detail.DockerWarning = "This project has no currently running containers."
		return detail
	}

	detail.DockerAvailable = true
	detail.Containers = live.Containers
	detail.Volumes = live.Volumes
	detail.Networks = live.Networks
	return detail
}

// validateProjectPath ensures path is an absolute, cleaned filesystem path,
// rejecting anything that could be used for path-traversal tricks (e.g. a
// path smuggling ".." segments past a UI-level prefix check elsewhere).
// filepath.Clean already resolves ".." segments lexically, so the returned
// path never contains them.
func validateProjectPath(path string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

func detectComposeFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	found := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			found[e.Name()] = true
		}
	}

	var files []string
	for _, name := range knownComposeFilenames {
		if found[name] {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files
}
