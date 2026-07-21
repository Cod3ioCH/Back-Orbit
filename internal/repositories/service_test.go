package repositories

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

const testPassphrase = "a-sufficiently-long-master-passphrase"

// newService builds a service backed by a real restic engine, so a repository
// reported as "ready" has genuinely been opened rather than merely recorded.
func newService(t *testing.T) (*Service, *secrets.Store, *sql.DB) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not installed; skipping repository integration test")
	}

	db := dbtest.Open(t)
	secretStore := secrets.NewStore(db)
	if err := secretStore.Initialize(context.Background(), testPassphrase); err != nil {
		t.Fatalf("initialise secret store: %v", err)
	}

	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	locations := NewLocations(filepath.Join(t.TempDir(), "data"), filepath.Join(t.TempDir(), "backups"))
	return NewService(db, secretStore, backup.NewResticEngine(""), recorder, locations), secretStore, db
}

func createLocal(t *testing.T, svc *Service, name string) Repository {
	t.Helper()
	repo, err := svc.Create(context.Background(), "actor", CreateRequest{
		Name:     name,
		Kind:     backup.RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "repo"),
		Password: "repository-password",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return repo
}

func TestCreateAndList(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	repo := createLocal(t, svc, "primary")
	if repo.ID == "" {
		t.Fatal("expected an id")
	}
	if repo.Status != StatusUnknown {
		t.Fatalf("a new repository must start unchecked, got %q", repo.Status)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != repo.ID {
		t.Fatalf("expected the created repository in the list, got %+v", list)
	}
}

// TestPasswordIsNotStoredWithTheRepository is the point of splitting the two:
// the repositories table says where backups go, never how to read them.
func TestPasswordIsNotStoredWithTheRepository(t *testing.T) {
	svc, _, db := newService(t)
	ctx := context.Background()

	const password = "unmistakable-repository-password-7c1e"
	repo, err := svc.Create(ctx, "actor", CreateRequest{
		Name:     "primary",
		Kind:     backup.RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "repo"),
		Password: password,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, name, kind, location, status, last_error FROM repositories`)
	if err != nil {
		t.Fatalf("query repositories: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, kind, location, status, lastError string
		if err := rows.Scan(&id, &name, &kind, &location, &status, &lastError); err != nil {
			t.Fatalf("scan: %v", err)
		}
		joined := strings.Join([]string{id, name, kind, location, status, lastError}, "|")
		if strings.Contains(joined, password) {
			t.Fatal("the repository password is readable in the repositories table")
		}
	}

	// It must, however, be retrievable through the secret store.
	config, err := svc.EngineConfig(ctx, repo.ID)
	if err != nil {
		t.Fatalf("EngineConfig: %v", err)
	}
	if config.Password != password {
		t.Fatal("the stored password did not round-trip through the secret store")
	}
}

// TestCheckReportsUninitializedThenReady walks the state a real operator sees:
// a configured destination that does not hold a repository yet, then the same
// destination after initialising it.
func TestCheckReportsUninitializedThenReady(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	repo := createLocal(t, svc, "primary")

	result, err := svc.Check(ctx, "actor", repo.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != StatusUninitialized {
		t.Fatalf("expected an empty destination to report uninitialized, got %q (%s)", result.Status, result.Error)
	}

	if err := svc.Initialize(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err = svc.Check(ctx, "actor", repo.ID)
	if err != nil {
		t.Fatalf("Check after initialise: %v", err)
	}
	if result.Status != StatusReady {
		t.Fatalf("expected ready after initialising, got %q (%s)", result.Status, result.Error)
	}

	// The observation must be persisted, not just returned.
	stored, err := svc.Get(ctx, repo.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != StatusReady {
		t.Fatalf("stored status = %q, want ready", stored.Status)
	}
	if stored.LastCheckedAt == nil {
		t.Fatal("expected the check timestamp to be recorded")
	}
}

// TestInitializeTwiceIsNotAnError covers a button an operator may well press
// again: the destination ends up usable either way, which is what was asked.
func TestInitializeTwiceIsNotAnError(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	repo := createLocal(t, svc, "primary")

	if err := svc.Initialize(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := svc.Initialize(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("initialising an existing repository should succeed, got %v", err)
	}

	stored, _ := svc.Get(ctx, repo.ID)
	if stored.Status != StatusReady {
		t.Fatalf("status = %q, want ready", stored.Status)
	}
}

// TestCheckRecordsFailure proves an unreachable destination is answered rather
// than thrown: the operator needs the reason, stored and visible.
func TestCheckRecordsFailure(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not restrict access, so the destination cannot be made unreadable")
	}

	// The destination is usable when the repository is configured and only
	// becomes unreadable afterwards — a mount that disappears or loses its
	// permissions, which is how this failure actually shows up in practice.
	parent := t.TempDir()
	location := filepath.Join(parent, "repo")

	repo, err := svc.Create(ctx, "actor", CreateRequest{
		Name:     "unreadable",
		Kind:     backup.RepositoryLocal,
		Location: location,
		Password: "repository-password",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("make destination unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	result, err := svc.Check(ctx, "actor", repo.ID)
	if err != nil {
		t.Fatalf("Check must answer rather than fail: %v", err)
	}
	if result.Status == StatusReady {
		t.Fatal("an unreachable destination must not report ready")
	}

	stored, _ := svc.Get(ctx, repo.ID)
	if stored.Status == StatusReady {
		t.Fatalf("stored status = %q, want a non-ready status", stored.Status)
	}
}

// TestLockedSecretStoreBlocksRepositoryAccess is the behaviour that makes the
// unattended-unlock argument concrete: with the store locked, nothing can
// reach a repository at all.
func TestLockedSecretStoreBlocksRepositoryAccess(t *testing.T) {
	svc, secretStore, _ := newService(t)
	ctx := context.Background()
	repo := createLocal(t, svc, "primary")

	secretStore.Lock()

	if _, err := svc.EngineConfig(ctx, repo.ID); !errors.Is(err, secrets.ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if _, err := svc.Check(ctx, "actor", repo.ID); !errors.Is(err, secrets.ErrLocked) {
		t.Fatalf("expected Check to report the locked store, got %v", err)
	}

	// Listing must still work: an operator has to be able to see what is
	// configured in order to know what to unlock.
	if list, err := svc.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("List while locked = %v, %v", list, err)
	}
}

// TestDeleteRemovesThePasswordToo guards against orphaned credentials
// accumulating in the secret store.
func TestDeleteRemovesThePasswordToo(t *testing.T) {
	svc, secretStore, _ := newService(t)
	ctx := context.Background()
	repo := createLocal(t, svc, "primary")

	if _, err := secretStore.Get(ctx, secrets.KindRepository, repo.ID); err != nil {
		t.Fatalf("the password should exist before delete: %v", err)
	}

	if err := svc.Delete(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := secretStore.Get(ctx, secrets.KindRepository, repo.ID); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected the password to be gone, got %v", err)
	}
	if _, err := svc.Get(ctx, repo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the repository to be gone, got %v", err)
	}
}

// TestDeleteLeavesTheDataAlone is a deliberate product decision: removing a
// configuration must never destroy someone's backups.
func TestDeleteLeavesTheDataAlone(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	location := filepath.Join(t.TempDir(), "repo")
	repo, err := svc.Create(ctx, "actor", CreateRequest{
		Name:     "primary",
		Kind:     backup.RepositoryLocal,
		Location: location,
		Password: "repository-password",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Initialize(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if err := svc.Delete(ctx, "actor", repo.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The restic repository on disk must still be there.
	if _, err := filepath.Glob(filepath.Join(location, "config")); err != nil {
		t.Fatalf("glob: %v", err)
	}
	entries, err := filepath.Glob(filepath.Join(location, "*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("deleting the configuration destroyed the repository data")
	}
}

func TestDuplicateNameIsRejected(t *testing.T) {
	svc, _, _ := newService(t)
	createLocal(t, svc, "primary")

	_, err := svc.Create(context.Background(), "actor", CreateRequest{
		Name:     "primary",
		Kind:     backup.RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "other"),
		Password: "another-password",
	})
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}
}

func TestCreateValidatesInput(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	cases := map[string]CreateRequest{
		"empty name":             {Kind: backup.RepositoryLocal, Location: "/tmp/repo", Password: "p"},
		"empty password":         {Name: "x", Kind: backup.RepositoryLocal, Location: "/tmp/repo"},
		"relative local path":    {Name: "x", Kind: backup.RepositoryLocal, Location: "relative/path", Password: "p"},
		"unknown kind":           {Name: "x", Kind: backup.RepositoryKind("ftp"), Location: "/tmp/repo", Password: "p"},
		"sftp not yet supported": {Name: "x", Kind: backup.RepositorySFTP, Location: "host:/srv", Password: "p"},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Create(ctx, "actor", req); err == nil {
				t.Fatal("expected the request to be rejected")
			}
		})
	}
}

// TestFailedCreateLeavesNoOrphanedSecret checks the rollback path: a create
// that fails after storing the password must not leave that password behind.
func TestFailedCreateLeavesNoOrphanedSecret(t *testing.T) {
	svc, secretStore, _ := newService(t)
	ctx := context.Background()

	createLocal(t, svc, "primary")

	before, err := secretStore.List(ctx)
	if err != nil {
		t.Fatalf("List secrets: %v", err)
	}

	// Same name, so the insert fails after the secret has been written.
	if _, err := svc.Create(ctx, "actor", CreateRequest{
		Name:     "primary",
		Kind:     backup.RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "other"),
		Password: "another-password",
	}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}

	after, err := secretStore.List(ctx)
	if err != nil {
		t.Fatalf("List secrets: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("a failed create left %d orphaned secret(s) behind", len(after)-len(before))
	}
}
