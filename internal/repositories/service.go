package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

// Service is the application-level entry point for repository management. It
// owns the relationship between a repository row, its password in the secret
// store, and the backup engine that talks to it.
type Service struct {
	store     *store
	secrets   *secrets.Store
	engine    backup.BackupEngine
	recorder  *events.Recorder
	locations *Locations
}

// NewService wires up a Service.
func NewService(db *sql.DB, secretStore *secrets.Store, engine backup.BackupEngine, recorder *events.Recorder, locations *Locations) *Service {
	return &Service{
		store:     newStore(db),
		secrets:   secretStore,
		engine:    engine,
		recorder:  recorder,
		locations: locations,
	}
}

// Locations reports where local repositories can be stored on this
// installation.
func (s *Service) Locations() []Location {
	if s.locations == nil {
		return nil
	}
	return s.locations.Suggest()
}

// List returns every configured repository. It works with the secret store
// locked, so an operator can always see what is configured.
func (s *Service) List(ctx context.Context) ([]Repository, error) {
	return s.store.list(ctx)
}

// Get returns one repository.
func (s *Service) Get(ctx context.Context, id string) (Repository, error) {
	return s.store.get(ctx, id)
}

// CreateRequest describes a new repository.
type CreateRequest struct {
	Name     string
	Kind     backup.RepositoryKind
	Location string
	Password string
}

// Create stores a repository and its password. The password goes straight
// into the encrypted secret store and is never written to the repositories
// table.
//
// Creating a repository does not initialise it or contact the destination:
// those are separate, explicit actions, so adding a configuration never has a
// side effect on storage the operator did not ask for.
//
// A local path is nevertheless checked for usability before it is stored.
// That check writes nothing and creates no directory, but it turns the two
// mistakes that are otherwise invisible until much later — an unwritable
// destination, and one that shares a volume with Back-Orbit's own database —
// into an answer while the operator is still looking at the form.
func (s *Service) Create(ctx context.Context, actorUserID string, req CreateRequest) (Repository, error) {
	if err := validateCreate(req); err != nil {
		return Repository{}, err
	}
	if req.Kind == backup.RepositoryLocal && s.locations != nil {
		if err := s.locations.validateLocalPath(strings.TrimSpace(req.Location)); err != nil {
			return Repository{}, err
		}
	}

	now := time.Now().UTC()
	repo := Repository{
		ID:        uuid.NewString(),
		Name:      strings.TrimSpace(req.Name),
		Kind:      req.Kind,
		Location:  strings.TrimSpace(req.Location),
		Status:    StatusUnknown,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// The secret is keyed by the repository's id, not its name, so renaming a
	// repository later cannot orphan its password.
	if _, err := s.secrets.Put(ctx, secrets.KindRepository, repo.ID, req.Password); err != nil {
		return Repository{}, err
	}

	if err := s.store.insert(ctx, repo); err != nil {
		// Roll the secret back so a failed create does not leave an
		// unreferenced password behind in the store.
		_ = s.secrets.Delete(ctx, secrets.KindRepository, repo.ID)
		return Repository{}, err
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionRepositoryCreated,
		ActorUserID: actorUserID,
		TargetType:  "repository",
		TargetID:    repo.ID,
		Metadata:    map[string]any{"name": repo.Name, "kind": string(repo.Kind)},
	})

	return repo, nil
}

// Delete removes a repository and its stored password.
//
// It deliberately does not touch the data at the destination. Deleting a
// configuration in Back-Orbit must never destroy someone's backups — removing
// the actual snapshots is a separate, explicit act performed with the tools
// that own that storage.
func (s *Service) Delete(ctx context.Context, actorUserID, id string) error {
	repo, err := s.store.get(ctx, id)
	if err != nil {
		return err
	}

	if err := s.store.delete(ctx, id); err != nil {
		return err
	}
	if err := s.secrets.Delete(ctx, secrets.KindRepository, id); err != nil {
		// The row is already gone; leaving the orphaned secret behind is
		// preferable to failing the whole delete, but it must be visible.
		return fmt.Errorf("repositories: repository removed but its password could not be deleted: %w", err)
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionRepositoryDeleted,
		ActorUserID: actorUserID,
		TargetType:  "repository",
		TargetID:    id,
		Metadata:    map[string]any{"name": repo.Name},
	})
	return nil
}

// Check contacts the repository and records what it found. A repository that
// cannot be reached is not an error from the caller's point of view — it is
// the answer to the question — so the failure is reported in the result and
// persisted as status, rather than returned as an error.
func (s *Service) Check(ctx context.Context, actorUserID, id string) (CheckResult, error) {
	config, repo, err := s.engineConfig(ctx, id)
	if err != nil {
		return CheckResult{}, err
	}

	checkedAt := time.Now().UTC()
	snapshots, err := s.engine.ListSnapshots(ctx, config)

	result := CheckResult{}
	switch {
	case err == nil:
		result.Status = StatusReady
		result.SnapshotCount = len(snapshots)
	case backup.KindOf(err) == backup.ErrKindRepositoryNotFound:
		// Reachable but empty is a normal state for a freshly configured
		// destination, and the fix is a button rather than a bug report.
		result.Status = StatusUninitialized
		result.Error = "no repository at this location yet"
	default:
		result.Status = StatusError
		result.Error = err.Error()
	}

	if updateErr := s.store.updateStatus(ctx, repo.ID, result.Status, result.Error, checkedAt); updateErr != nil {
		return result, updateErr
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionRepositoryChecked,
		ActorUserID: actorUserID,
		TargetType:  "repository",
		TargetID:    repo.ID,
		Metadata:    map[string]any{"name": repo.Name, "status": string(result.Status)},
	})

	return result, nil
}

// Initialize creates the repository at its destination.
func (s *Service) Initialize(ctx context.Context, actorUserID, id string) error {
	config, repo, err := s.engineConfig(ctx, id)
	if err != nil {
		return err
	}

	if err := s.engine.InitRepository(ctx, config); err != nil {
		now := time.Now().UTC()
		// An already-initialised repository is not a failure to report as one:
		// the destination is usable, which is what the operator wanted.
		if backup.KindOf(err) == backup.ErrKindRepositoryExists {
			_ = s.store.updateStatus(ctx, repo.ID, StatusReady, "", now)
			return nil
		}
		_ = s.store.updateStatus(ctx, repo.ID, StatusError, err.Error(), now)
		return err
	}

	if err := s.store.updateStatus(ctx, repo.ID, StatusReady, "", time.Now().UTC()); err != nil {
		return err
	}

	s.recorder.Emit(ctx, events.Event{
		Action:      events.ActionRepositoryInitialized,
		ActorUserID: actorUserID,
		TargetType:  "repository",
		TargetID:    repo.ID,
		Metadata:    map[string]any{"name": repo.Name},
	})
	return nil
}

// EngineConfig returns everything the backup engine needs to talk to a
// repository, including its password. It is exported for the backup and
// restore paths, which need it for the duration of a single operation.
//
// The returned config carries a live credential: keep it on the stack, pass it
// straight to the engine, and never log it or put it in a response.
func (s *Service) EngineConfig(ctx context.Context, id string) (backup.RepositoryConfig, error) {
	config, _, err := s.engineConfig(ctx, id)
	return config, err
}

func (s *Service) engineConfig(ctx context.Context, id string) (backup.RepositoryConfig, Repository, error) {
	repo, err := s.store.get(ctx, id)
	if err != nil {
		return backup.RepositoryConfig{}, Repository{}, err
	}

	password, err := s.secrets.Get(ctx, secrets.KindRepository, repo.ID)
	if err != nil {
		if errors.Is(err, secrets.ErrLocked) {
			return backup.RepositoryConfig{}, Repository{}, err
		}
		return backup.RepositoryConfig{}, Repository{},
			fmt.Errorf("repositories: read password for %q: %w", repo.Name, err)
	}

	return backup.RepositoryConfig{
		Kind:     repo.Kind,
		Location: repo.Location,
		Password: password,
	}, repo, nil
}

func validateCreate(req CreateRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidConfig)
	}
	if req.Password == "" {
		return fmt.Errorf("%w: password must not be empty", ErrInvalidConfig)
	}

	location := strings.TrimSpace(req.Location)
	if location == "" {
		return fmt.Errorf("%w: location must not be empty", ErrInvalidConfig)
	}

	switch req.Kind {
	case backup.RepositoryLocal:
		// An absolute path keeps the meaning of the location independent of
		// whatever working directory the process happens to have.
		if !filepath.IsAbs(location) {
			return fmt.Errorf("%w: a local repository path must be absolute", ErrInvalidConfig)
		}
	case backup.RepositorySFTP, backup.RepositoryS3:
		// Reachable in the engine, but not offered yet: without validating the
		// endpoint against the SSRF concerns in docs/threat-model.md, accepting
		// a URL here would let an admin-supplied address reach internal
		// services. Those checks land with the phase that adds these kinds.
		return fmt.Errorf("%w: %s repositories are not supported yet", ErrInvalidKind, req.Kind)
	default:
		return ErrInvalidKind
	}

	return nil
}
