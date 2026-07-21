package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeResticLayout creates the files restic itself puts in a repository, so a
// test exercises the same signature the purge guard looks for.
func writeResticLayout(t *testing.T, dir string) {
	t.Helper()
	for _, sub := range []string{"data", "index", "keys", "locks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keys", "abc123"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

func TestPurgeRemovesARealRepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	writeResticLayout(t, dir)

	deleted, err := purgeLocalRepository(dir)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !deleted {
		t.Error("deleting a populated repository must report that data was removed")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the repository directory is still there")
	}
}

// TestPurgeRefusesForeignData is the guard that matters most: a location that
// is not a restic repository holds something Back-Orbit never created, and
// erasing it would destroy data it cannot even describe.
func TestPurgeRefusesForeignData(t *testing.T) {
	root := t.TempDir()

	cases := map[string]func(t *testing.T) string{
		"someone's documents": func(t *testing.T) string {
			dir := filepath.Join(root, "documents")
			mkdir(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "taxes.pdf"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir
		},
		"a config file but no keys": func(t *testing.T) string {
			dir := filepath.Join(root, "half")
			mkdir(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "config"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir
		},
		"a file, not a directory": func(t *testing.T) string {
			path := filepath.Join(root, "just-a-file")
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			path := setup(t)

			deleted, err := purgeLocalRepository(path)

			if !errors.Is(err, ErrNotARepository) {
				t.Fatalf("err = %v, want ErrNotARepository", err)
			}
			if deleted {
				t.Error("nothing may be reported as deleted when the location was refused")
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the refused location was touched anyway: %v", err)
			}
		})
	}
}

// TestPurgeToleratesNothingThere covers a repository that was configured but
// never initialised: there is no data, and that is not an error.
func TestPurgeToleratesNothingThere(t *testing.T) {
	deleted, err := purgeLocalRepository(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("a location that does not exist must not fail: %v", err)
	}
	if deleted {
		t.Error("nothing was there, so nothing can have been deleted")
	}
}

func TestPurgeRemovesAnEmptyDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	mkdir(t, dir)

	deleted, err := purgeLocalRepository(dir)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted {
		t.Error("an empty directory holds no data, so nothing was destroyed")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the empty directory should still have been cleaned up")
	}
}

// TestPurgeFollowsSymlinksToTheirTarget makes sure a link cannot be used to
// point the deletion somewhere it was never configured. The link resolves to
// foreign data, so the guard must refuse it — checking the link's own name
// would have missed this entirely.
func TestPurgeSymlinkCannotRedirectToForeignData(t *testing.T) {
	root := t.TempDir()

	victim := filepath.Join(root, "important-data")
	mkdir(t, victim)
	if err := os.WriteFile(filepath.Join(victim, "irreplaceable.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "looks-like-a-repo")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := purgeLocalRepository(link); !errors.Is(err, ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if _, err := os.Stat(filepath.Join(victim, "irreplaceable.txt")); err != nil {
		t.Fatalf("the symlink target was destroyed: %v", err)
	}
}
