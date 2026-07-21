// Package dbdump exports databases into a form that can be restored, rather
// than copying the files a running server is in the middle of writing.
//
// A file-level copy of a live database is a copy of a moving target: pages
// written, a write-ahead log half-applied, and no guarantee the result was ever
// a state the database was in. It restores into something that may not start.
// A logical dump asks the database itself for a consistent view, which is the
// only thing that makes the copy trustworthy.
package dbdump

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// Result describes a dump that was written.
type Result struct {
	// Technology is the engine that was dumped.
	Technology string `json:"technology"`
	// Service is the Compose service it belongs to.
	Service string `json:"service"`
	// Path is where the dump sits inside the staged tree.
	Path string `json:"path"`
	// Command is what produced it, so a restore does not have to guess how to
	// read the file back.
	Command string `json:"command"`
	// User is the account the export was taken as, and the one a replay should
	// use. Never a password.
	User string `json:"user"`
	// Bytes is the size of the dump.
	Bytes int64 `json:"bytes"`
	// Duration is how long it took.
	Duration time.Duration `json:"-"`
}

// Target identifies a database to dump.
type Target struct {
	Technology  string
	Service     string
	ContainerID string
	// User is the database superuser, taken from the container's own
	// configuration.
	User string

	// Password is required by the MySQL family, which does not trust the
	// container's operating-system user the way PostgreSQL does. It is passed
	// to the tool through the process environment, never on the command line,
	// and is never logged or returned.
	Password string
}

// dumpDirectory is where dumps are placed inside the staged tree.
//
// A directory of its own, beside the volume copies rather than inside one, so
// a restore can tell the export from the raw files it was taken instead of.
const dumpDirectory = "back-orbit-dumps"

// PostgreSQL writes a logical dump of every database and role in a PostgreSQL
// server into stagingDir.
//
// The dump runs inside the database's own container. That is deliberate and it
// is not the obvious choice: the original plan was a separate helper container,
// on the reasoning that an arbitrary image may not carry the dump tools. For
// PostgreSQL the opposite risk dominates — pg_dump refuses to dump a server
// newer than itself, so a helper carrying some other version fails precisely
// when the backup is needed. The binaries beside the running server always
// match it.
//
// pg_dumpall rather than pg_dump: roles, ownership and grants live outside any
// single database, and a dump that restores tables into a server with no users
// to own them is not a restore of the system anyone had.
func PostgreSQL(ctx context.Context, client docker.Client, target Target, stagingDir string) (Result, error) {
	if target.ContainerID == "" {
		return Result{}, fmt.Errorf("dbdump: no running container for service %q", target.Service)
	}

	user := strings.TrimSpace(target.User)
	if user == "" {
		// The image's own default, which is what an unconfigured PostgreSQL
		// container runs as.
		user = "postgres"
	}
	if err := validIdentifier(user); err != nil {
		return Result{}, fmt.Errorf("dbdump: refusing database user %q: %w", target.User, err)
	}

	relative := filepath.Join(dumpDirectory, fmt.Sprintf("%s-postgresql.sql", pathSegment(target.Service)))
	absolute := filepath.Join(stagingDir, relative)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump directory: %w", err)
	}

	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump file: %w", err)
	}
	defer file.Close()

	// An argument vector, never a shell string. --clean and --if-exists make
	// the dump replayable into a server that already has objects, which is the
	// situation a restore is actually performed in.
	command := []string{"pg_dumpall", "--clean", "--if-exists", "--username", user}

	started := time.Now()
	run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
		Cmd:    command,
		Stdout: file,
	})
	if err != nil {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: run pg_dumpall: %w", err)
	}
	if run.ExitCode != 0 {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: pg_dumpall exited %d: %s",
			run.ExitCode, strings.TrimSpace(run.Stderr))
	}

	if err := file.Sync(); err != nil {
		return Result{}, fmt.Errorf("dbdump: flush dump: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Result{}, fmt.Errorf("dbdump: measure dump: %w", err)
	}

	// pg_dumpall can exit zero having written nothing if it was pointed at
	// something that is not a server. An empty dump that travels into a
	// snapshot as a successful export is the worst of both worlds.
	if info.Size() == 0 {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: pg_dumpall produced an empty dump")
	}

	return Result{
		Technology: "postgresql",
		Service:    target.Service,
		Path:       relative,
		Command:    strings.Join(command, " "),
		User:       user,
		Bytes:      info.Size(),
		Duration:   time.Since(started),
	}, nil
}

// removeFailedDump deletes a partial dump, so nothing that failed can end up
// in a snapshot looking like an export.
func removeFailedDump(path string) {
	_ = os.Remove(path)
}

// validIdentifier rejects a database user name that is not one.
//
// The value comes from the container's environment, which comes from a Compose
// file. It is passed as its own argument and never through a shell, so this is
// a second line rather than the only one — but a name carrying a newline or a
// leading dash would still be a surprise worth refusing.
func validIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("it is empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("it starts with a dash and would be read as an option")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
		default:
			return fmt.Errorf("it contains %q", r)
		}
	}
	return nil
}

// pathSegment turns a service name into one safe file-name component.
func pathSegment(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	segment := strings.Trim(b.String(), "-.")
	if segment == "" {
		return "database"
	}
	return segment
}
