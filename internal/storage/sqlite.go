package storage

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// sqliteMagic opens every SQLite database file, whatever it is named.
//
// Matching on content rather than on ".db" or ".sqlite" matters: applications
// name these files anything at all, and the ones that would be missed by an
// extension list are exactly the ones nobody thinks to check.
var sqliteMagic = []byte("SQLite format 3\x00")

// sqliteCapturePath is where consistent copies are written inside the helper
// container — its own writable layer, never the user's directory.
//
// Under /tmp because helper containers run as Back-Orbit's unprivileged image
// user, which cannot create directories at the container root.
const sqliteCapturePath = "/tmp/back-orbit-capture"

// SQLiteCapture records what happened to one database.
type SQLiteCapture struct {
	// Path is the database's location relative to the staged directory.
	Path string `json:"path"`
	// Method is how it was captured, in words a restore can rely on.
	Method string `json:"method"`
	// Bytes is the size of the captured copy.
	Bytes int64 `json:"bytes"`
}

// captureSQLiteDatabases replaces the naively copied SQLite files in a staged
// directory with transactionally consistent copies.
//
// This exists because copying a live SQLite database is not a backup. In WAL
// mode — the default for most applications that care about concurrency — the
// most recent commits live in a separate "-wal" file, and the three files that
// make up the database are copied one after another while the application
// keeps writing. The result can be a set that never existed as a coherent
// state: a database that opens, reports no error, and is missing or mixing
// transactions. The failure is silent at backup time and only appears when the
// backup is restored, which is the worst possible moment to discover it.
//
// The fix is to let SQLite take the copy, through its own online backup API,
// which holds the right locks and follows the WAL. That requires SQLite to
// *open* the database, and opening a WAL database means being able to create
// its shared-memory file — so the source is mounted read-write for this step
// alone. Nothing writes to the database's contents; the only files SQLite may
// touch are the "-shm" and "-wal" the running application already maintains.
//
// The consistent copy is written inside the helper container and read back out
// through the archive API, so the user's directory never receives a file from
// Back-Orbit.
func (s *Stager) captureSQLiteDatabases(ctx context.Context, source, stagedDir string) ([]SQLiteCapture, []string, error) {
	databases, err := findSQLiteDatabases(stagedDir)
	if err != nil {
		return nil, nil, err
	}
	if len(databases) == 0 {
		return nil, nil, nil
	}

	image, err := s.resolveHelperImage(ctx)
	if err != nil {
		return nil, nil, err
	}

	// One container for all of them. A container per database was the obvious
	// shape and the wrong one: this project alone holds nine, and each start
	// costs about a second of pure overhead for work that is identical.
	captured, err := s.runSQLiteCapture(ctx, image, source, databases)
	if err != nil {
		// The whole capture step failed, so no database in this source is
		// trustworthy. Every one is reported rather than the backup quietly
		// containing plain file copies.
		warnings := make([]string, 0, len(databases))
		for _, relative := range databases {
			warnings = append(warnings, sqliteWarning(relative, err))
		}
		return nil, warnings, nil
	}

	var (
		captures []SQLiteCapture
		warnings []string
	)
	for index, relative := range databases {
		data, ok := captured[strconv.Itoa(index)]
		if !ok || len(data) == 0 {
			warnings = append(warnings, sqliteWarning(relative,
				errors.New("sqlite3 produced no output for it")))
			continue
		}

		target := filepath.Join(stagedDir, relative)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			warnings = append(warnings, sqliteWarning(relative, err))
			continue
		}

		// The sidecars belonged to the file copy. The captured database is a
		// complete, checkpointed copy, and leaving a stale "-wal" beside it
		// would make SQLite try to replay a log that no longer matches.
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if err := os.Remove(target + suffix); err != nil && !os.IsNotExist(err) {
				warnings = append(warnings, sqliteWarning(relative, err))
			}
		}

		captures = append(captures, SQLiteCapture{
			Path:   relative,
			Method: "sqlite3 online backup (consistent, includes the write-ahead log)",
			Bytes:  int64(len(data)),
		})
	}

	return captures, warnings, nil
}

func sqliteWarning(relative string, err error) string {
	return fmt.Sprintf("%s is a SQLite database that could not be captured consistently (%v); "+
		"the plain file copy in this snapshot may be missing recent transactions or be "+
		"unusable if the application was writing during the backup", relative, err)
}

// runSQLiteCapture backs up every database in one helper container and returns
// the results keyed by their index in databases.
func (s *Stager) runSQLiteCapture(ctx context.Context, image, source string, databases []string) (map[string][]byte, error) {
	// The databases are passed as positional arguments, never interpolated
	// into the script: these are file names from a user's own directory, and
	// one containing a quote or a semicolon must not be able to become a
	// command. The only interpolation is a constant path.
	//
	// A failure on one database is left to be detected from its missing output
	// rather than aborting the loop, so one unreadable file cannot cost the
	// consistent capture of every other.
	script := "mkdir -p " + sqliteCapturePath + `
i=0
for db in "$@"; do
  sqlite3 "$db" ".backup '` + sqliteCapturePath + `/$i'" || echo "back-orbit: failed $db" >&2
  i=$((i+1))
done`

	command := append([]string{"/bin/sh", "-c", script, "sh"}, containerPaths(databases)...)

	containerID, err := s.docker.CreateHelperContainer(ctx, docker.HelperContainerRequest{
		Image:     image,
		Source:    source,
		MountPath: mountPath,
		// The one place staging mounts a source read-write. SQLite cannot open
		// a write-ahead-log database without being able to create its shared
		// memory file, and that is the only way to read it consistently.
		Writable: true,
		Command:  command,
		Purpose:  "capture-sqlite",
	})
	if err != nil {
		return nil, fmt.Errorf("create helper: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if removeErr := s.docker.RemoveContainer(cleanupCtx, containerID); removeErr != nil {
			slog.Error("storage: could not remove the SQLite capture container",
				"container", containerID, "error", removeErr)
		}
	}()

	run, err := s.docker.RunHelperContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("run sqlite3: %w", err)
	}
	if run.ExitCode != 0 {
		return nil, fmt.Errorf("sqlite3 exited %d: %s", run.ExitCode, strings.TrimSpace(run.Output))
	}

	archive, err := s.docker.ContainerArchive(ctx, containerID, sqliteCapturePath)
	if err != nil {
		return nil, fmt.Errorf("read the captured copies: %w", err)
	}
	defer archive.Close()

	return readCaptureArchive(archive)
}

// containerPaths maps staged-relative paths to where they are inside the
// helper container.
func containerPaths(relatives []string) []string {
	paths := make([]string, 0, len(relatives))
	for _, relative := range relatives {
		paths = append(paths, mountPath+"/"+filepath.ToSlash(relative))
	}
	return paths
}

// readCaptureArchive reads the tar of captured databases, keyed by file name.
func readCaptureArchive(r io.Reader) (map[string][]byte, error) {
	captured := map[string][]byte{}
	reader := tar.NewReader(r)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			return captured, nil
		}
		if err != nil {
			return nil, fmt.Errorf("storage: read the captured copies: %w", err)
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("storage: read captured %s: %w", header.Name, err)
		}
		captured[path.Base(header.Name)] = data
	}
}

// findSQLiteDatabases returns the staged files that are SQLite databases,
// relative to root.
func findSQLiteDatabases(root string) ([]string, error) {
	var found []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		// The sidecars are not databases in their own right and are handled
		// with the database they belong to.
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if strings.HasSuffix(path, suffix) {
				return nil
			}
		}

		isDB, err := hasSQLiteMagic(path)
		if err != nil {
			return err
		}
		if !isDB {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = append(found, relative)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage: scan for SQLite databases: %w", err)
	}
	return found, nil
}

func hasSQLiteMagic(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	header := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(file, header)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return false, nil // too small to be a database
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(header[:n], sqliteMagic), nil
}
