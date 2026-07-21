package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeKeyFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	// WriteFile is subject to umask, so set the mode explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod key file: %v", err)
	}
	return path
}

// TestUnlockFromFile covers the restart path that keeps scheduled backups
// running: no human is present, so the passphrase has to come from a mounted
// secret.
func TestUnlockFromFile(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "primary", "repository password"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	path := writeKeyFile(t, testPassphrase, 0o600)

	restarted := NewStore(db)
	if err := restarted.UnlockFromFile(ctx, path); err != nil {
		t.Fatalf("UnlockFromFile: %v", err)
	}
	if !restarted.IsUnlocked() {
		t.Fatal("the store must be unlocked after reading the key file")
	}

	got, err := restarted.Get(ctx, KindRepository, "primary")
	if err != nil {
		t.Fatalf("Get after unattended unlock: %v", err)
	}
	if got != "repository password" {
		t.Fatalf("Get = %q", got)
	}
}

// TestUnlockFromFileIgnoresTrailingNewline matters in practice: `echo secret >
// file` and most editors append one, and an unlock that fails over an
// invisible byte is impossible to debug.
func TestUnlockFromFileIgnoresTrailingNewline(t *testing.T) {
	store, db := newInitializedStore(t)
	store.Lock()

	path := writeKeyFile(t, testPassphrase+"\n", 0o600)

	restarted := NewStore(db)
	if err := restarted.UnlockFromFile(context.Background(), path); err != nil {
		t.Fatalf("UnlockFromFile with trailing newline: %v", err)
	}
}

// TestUnlockFromFileRejectsWorldReadableFile guards against the quiet
// misconfiguration: a key file everyone can read makes encrypting the secrets
// pointless.
func TestUnlockFromFileRejectsWorldReadableFile(t *testing.T) {
	_, db := newInitializedStore(t)
	path := writeKeyFile(t, testPassphrase, 0o644)

	store := NewStore(db)
	err := store.UnlockFromFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected a world-readable key file to be refused")
	}
	if store.IsUnlocked() {
		t.Fatal("the store must stay locked when the key file is unsafe")
	}
}

func TestUnlockFromFileRejectsGroupReadableFile(t *testing.T) {
	_, db := newInitializedStore(t)
	path := writeKeyFile(t, testPassphrase, 0o640)

	if err := NewStore(db).UnlockFromFile(context.Background(), path); err == nil {
		t.Fatal("expected a group-readable key file to be refused")
	}
}

func TestUnlockFromFileWithoutPathIsReported(t *testing.T) {
	_, db := newInitializedStore(t)
	if err := NewStore(db).UnlockFromFile(context.Background(), "  "); !errors.Is(err, ErrNoKeyFile) {
		t.Fatalf("expected ErrNoKeyFile, got %v", err)
	}
}

func TestUnlockFromFileRejectsEmptyFile(t *testing.T) {
	_, db := newInitializedStore(t)
	path := writeKeyFile(t, "", 0o600)

	if err := NewStore(db).UnlockFromFile(context.Background(), path); err == nil {
		t.Fatal("expected an empty key file to be refused")
	}
}

func TestUnlockFromFileRejectsDirectory(t *testing.T) {
	_, db := newInitializedStore(t)
	dir := t.TempDir()

	if err := NewStore(db).UnlockFromFile(context.Background(), dir); err == nil {
		t.Fatal("expected a directory to be refused as a key file")
	}
}

func TestUnlockFromFileRejectsOversizedFile(t *testing.T) {
	_, db := newInitializedStore(t)
	path := writeKeyFile(t, string(make([]byte, maxKeyFileSize+1)), 0o600)

	if err := NewStore(db).UnlockFromFile(context.Background(), path); err == nil {
		t.Fatal("expected an implausibly large key file to be refused")
	}
}

func TestUnlockFromFileRejectsWrongPassphrase(t *testing.T) {
	_, db := newInitializedStore(t)
	path := writeKeyFile(t, "not-the-master-passphrase", 0o600)

	store := NewStore(db)
	if err := store.UnlockFromFile(context.Background(), path); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

func TestUnlockFromFileMissingFile(t *testing.T) {
	_, db := newInitializedStore(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	if err := NewStore(db).UnlockFromFile(context.Background(), missing); err == nil {
		t.Fatal("expected a missing key file to be reported")
	}
}
