package dbdump

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// MongoDB writes an archive of a MongoDB server into stagingDir.
//
// `--archive` streams one self-contained file rather than a directory tree,
// which is what lets the export travel inside the same snapshot as everything
// else instead of becoming a folder of thousands of BSON files.
//
// The credentials are the awkward part. mongodump has no environment variable
// for a password: the documented ways in are the command line, a config file,
// or an interactive prompt. Command line is out — argv is readable by any
// process on the host through `ps`. A config file written into the container
// would leave the password on disk if anything died before cleaning it up. So
// the config goes in on standard input, as /dev/stdin, and exists only in the
// pipe between the two processes.
//
// An unauthenticated server — the default for the official image when no root
// user is configured, and common inside a Compose network — needs none of
// this, and is dumped without credentials at all.
func MongoDB(ctx context.Context, client docker.Client, target Target, stagingDir string) (Result, error) {
	if target.ContainerID == "" {
		return Result{}, fmt.Errorf("dbdump: no running container for service %q", target.Service)
	}

	relative := filepath.Join(dumpDirectory, fmt.Sprintf("%s-mongodb.archive", pathSegment(target.Service)))
	absolute := filepath.Join(stagingDir, relative)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump directory: %w", err)
	}

	command := []string{"mongodump", "--archive"}
	var stdin io.Reader

	user := strings.TrimSpace(target.User)
	if user != "" {
		if err := validIdentifier(user); err != nil {
			return Result{}, fmt.Errorf("dbdump: refusing database user %q: %w", target.User, err)
		}
		command = append(command,
			"--username", user,
			// Root users created by the official image live in "admin",
			// whichever database they go on to administer.
			"--authenticationDatabase", "admin",
		)
		if target.Password != "" {
			command = append(command, "--config", "/dev/stdin")
			// A single YAML key. The password is quoted so that a value
			// containing a colon or a leading brace cannot change the
			// document's shape.
			stdin = strings.NewReader("password: " + quoteYAML(target.Password) + "\n")
		}
	}

	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump file: %w", err)
	}
	defer file.Close()

	started := time.Now()
	run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
		Cmd:    command,
		Stdin:  stdin,
		Stdout: file,
	})
	if err != nil {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: run mongodump: %w", err)
	}
	if run.ExitCode != 0 {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: mongodump exited %d: %s",
			run.ExitCode, redactSecret(strings.TrimSpace(run.Stderr), target.Password))
	}

	if err := file.Sync(); err != nil {
		return Result{}, fmt.Errorf("dbdump: flush dump: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Result{}, fmt.Errorf("dbdump: measure dump: %w", err)
	}
	if info.Size() == 0 {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: mongodump produced an empty archive")
	}

	return Result{
		Technology: "mongodb",
		Service:    target.Service,
		Path:       relative,
		// Recorded without the config redirection, which is an artefact of how
		// the password was delivered rather than part of what was dumped.
		Command:  strings.Join(withoutConfigFlag(command), " "),
		User:     user,
		Bytes:    info.Size(),
		Duration: time.Since(started),
	}, nil
}

// withoutConfigFlag drops "--config /dev/stdin" from a recorded command.
func withoutConfigFlag(command []string) []string {
	out := make([]string, 0, len(command))
	for i := 0; i < len(command); i++ {
		if command[i] == "--config" {
			i++ // also skip its value
			continue
		}
		out = append(out, command[i])
	}
	return out
}

// quoteYAML wraps a value in single quotes, doubling any it contains.
//
// The password is the one piece of this document Back-Orbit did not write. An
// unquoted value containing a colon or a leading bracket would be read as
// structure rather than text, and the dump would fail in a way nobody could
// explain from the error.
func quoteYAML(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// redactSecret keeps a password out of an error message that travels into
// warnings, the audit log and the UI.
func redactSecret(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}
