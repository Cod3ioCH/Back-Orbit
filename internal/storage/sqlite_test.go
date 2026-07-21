package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSQLiteDetectionUsesContentNotNames is the point of detecting by magic
// bytes. Applications name their databases anything at all — the ones an
// extension list would miss are exactly the ones nobody thinks to check.
func TestSQLiteDetectionUsesContentNotNames(t *testing.T) {
	root := t.TempDir()

	database := append([]byte(nil), sqliteMagic...)
	database = append(database, make([]byte, 100)...)

	// Real databases with unhelpful names.
	writeTestFile(t, filepath.Join(root, "buergergemeinde.db"), database)
	writeTestFile(t, filepath.Join(root, "store"), database)
	writeTestFile(t, filepath.Join(root, "nested", "state.bin"), database)

	// Not databases, including one named to look like one.
	writeTestFile(t, filepath.Join(root, "notes.txt"), []byte("hello"))
	writeTestFile(t, filepath.Join(root, "decoy.db"), []byte("this is not a database"))
	writeTestFile(t, filepath.Join(root, "tiny"), []byte("x"))

	// Sidecars belong to their database and must not be listed separately.
	writeTestFile(t, filepath.Join(root, "buergergemeinde.db-wal"), database)
	writeTestFile(t, filepath.Join(root, "buergergemeinde.db-shm"), database)

	found, err := findSQLiteDatabases(root)
	if err != nil {
		t.Fatalf("findSQLiteDatabases: %v", err)
	}

	got := map[string]bool{}
	for _, path := range found {
		got[path] = true
	}

	for _, want := range []string{"buergergemeinde.db", "store", filepath.Join("nested", "state.bin")} {
		if !got[want] {
			t.Errorf("%s is a SQLite database but was not detected", want)
		}
	}
	for _, unwanted := range []string{"notes.txt", "decoy.db", "tiny",
		"buergergemeinde.db-wal", "buergergemeinde.db-shm"} {
		if got[unwanted] {
			t.Errorf("%s was treated as a database", unwanted)
		}
	}
}

func TestSQLiteDetectionHandlesAnEmptyTree(t *testing.T) {
	found, err := findSQLiteDatabases(t.TempDir())
	if err != nil {
		t.Fatalf("an empty tree must not be an error: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("got %v, want nothing", found)
	}
}
