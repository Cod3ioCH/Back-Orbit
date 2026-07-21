package repositories

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepositoryInsideDataDirIsRefused covers the misconfiguration with the
// worst consequences: backups sitting in the same volume as Back-Orbit's own
// database look like they are working, right up until the volume is lost and
// takes both with it. Refusing it is the only outcome that protects anything.
func TestRepositoryInsideDataDirIsRefused(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backups")
	mkdir(t, dataDir, backupDir)

	locations := NewLocations(dataDir, backupDir)

	for name, path := range map[string]string{
		"directly inside": filepath.Join(dataDir, "repo"),
		"deeper inside":   filepath.Join(dataDir, "nested", "repo"),
		"the data dir":    dataDir,
		"a parent of it":  root,
	} {
		t.Run(name, func(t *testing.T) {
			err := locations.validateLocalPath(path)
			if err == nil {
				t.Fatalf("validateLocalPath(%q) = nil, want refusal", path)
			}
			// The operator has to be told where to go instead, otherwise a
			// refusal just moves the guessing game one step along.
			if !strings.Contains(err.Error(), backupDir) {
				t.Errorf("refusal does not point at the usable location %q: %v", backupDir, err)
			}
		})
	}
}

// TestSiblingOfDataDirIsAccepted guards the containment check against matching
// on a string prefix: "/srv/data-backups" is not inside "/srv/data".
func TestSiblingOfDataDirIsAccepted(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	sibling := filepath.Join(root, "data-backups")
	mkdir(t, dataDir, sibling)

	// backupDir is deliberately elsewhere, so acceptance cannot come from the
	// path happening to be the suggested one.
	locations := NewLocations(dataDir, filepath.Join(root, "elsewhere"))

	if err := locations.validateLocalPath(filepath.Join(sibling, "repo")); err != nil {
		t.Fatalf("a sibling directory must be accepted: %v", err)
	}
}

// TestUnwritableLocationIsRefusedWithReason is the case the operator actually
// hit: a path that looks fine and fails at the first backup.
func TestUnwritableLocationIsRefusedWithReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not restrict access")
	}

	root := t.TempDir()
	readOnly := filepath.Join(root, "read-only")
	mkdir(t, readOnly)
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })

	locations := NewLocations(filepath.Join(root, "data"), filepath.Join(root, "backups"))

	err := locations.validateLocalPath(filepath.Join(readOnly, "repo"))
	if err == nil {
		t.Fatal("an unwritable location must be refused at creation, not at the first backup")
	}
	if !strings.Contains(err.Error(), readOnly) {
		t.Errorf("refusal must name the directory that is the problem: %v", err)
	}
}

// TestValidationCreatesNothing keeps checking a location free of side effects:
// deciding whether a path is usable must not start making it so.
func TestValidationCreatesNothing(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	mkdir(t, backupDir)

	locations := NewLocations(filepath.Join(root, "data"), backupDir)
	target := filepath.Join(backupDir, "not-created-yet", "repo")

	if err := locations.validateLocalPath(target); err != nil {
		t.Fatalf("a creatable path must be accepted: %v", err)
	}

	if _, err := os.Stat(filepath.Join(backupDir, "not-created-yet")); !os.IsNotExist(err) {
		t.Fatal("validation created a directory; it must only inspect")
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("validation left %d entries behind, including probe files", len(entries))
	}
}

// TestSuggestReportsRealWritability makes sure the UI is told the truth rather
// than an assumption: a suggestion that is not writable is worse than none.
func TestSuggestReportsRealWritability(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not restrict access")
	}

	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	mkdir(t, backupDir)

	locations := NewLocations(filepath.Join(root, "data"), backupDir)

	suggestions := locations.Suggest()
	if len(suggestions) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(suggestions))
	}
	if !suggestions[0].Writable {
		t.Fatalf("a writable directory must be reported as writable: %+v", suggestions[0])
	}

	if err := os.Chmod(backupDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(backupDir, 0o700) })

	suggestions = locations.Suggest()
	if suggestions[0].Writable {
		t.Fatal("an unwritable directory must not be reported as writable")
	}
	if suggestions[0].Detail == "" {
		t.Fatal("an unwritable suggestion must explain why, or it cannot be fixed")
	}
}

func mkdir(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
}
