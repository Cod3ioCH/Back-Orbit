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

// mysqlDumpTools are the dump binaries to try, in order, per engine.
//
// The name is not stable across the family. MariaDB 11 removed mysqldump
// entirely and ships only mariadb-dump; MySQL 8 ships only mysqldump; MariaDB
// 10 has mysqldump and may not have mariadb-dump. Picking one name would fail
// on half the installations out there, so both are tried and the first that
// exists wins.
var mysqlDumpTools = map[string][]string{
	"mysql":   {"mysqldump", "mariadb-dump"},
	"mariadb": {"mariadb-dump", "mysqldump"},
}

// mysqlClients are the interactive clients to try, in the same order and for
// the same reason as the dump tools.
var mysqlClients = map[string][]string{
	"mysql":   {"mysql", "mariadb"},
	"mariadb": {"mariadb", "mysql"},
}

// systemSchemas are never dumped.
//
// This is not tidiness, it is what makes the dump restorable. mysqldump
// --all-databases includes the `mysql` schema, whose user table carries the
// source server's credentials. Replaying that into a running server replaces
// its accounts mid-stream, the importing session loses the authorisation it is
// using, and the import dies — before reaching any user database, because
// `mysql` sorts ahead of most names. The dump looks complete and restores
// nothing.
//
// PostgreSQL is handled differently on purpose: pg_dumpall emits roles as
// CREATE ROLE statements that a target can absorb, rather than overwriting a
// system catalogue wholesale. The engines differ, so the treatment does.
var systemSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

// MySQL writes a logical dump of every database in a MySQL or MariaDB server
// into stagingDir.
//
// Unlike PostgreSQL, these servers do not trust the container's operating
// system user, so a password is required. It is passed through the process
// environment and never on the command line: argv is readable by any process
// on the host through `ps`, a process environment is not.
//
// --single-transaction takes the dump inside one consistent read view, so a
// live InnoDB server can be exported without locking the application out of
// its own database. It does not cover MyISAM tables, which cannot be dumped
// consistently while being written; the command used is recorded with the
// dump so that limitation is visible rather than assumed away.
func MySQL(ctx context.Context, client docker.Client, target Target, stagingDir string) (Result, error) {
	if target.ContainerID == "" {
		return Result{}, fmt.Errorf("dbdump: no running container for service %q", target.Service)
	}

	engine := target.Technology
	tools, known := mysqlDumpTools[engine]
	if !known {
		return Result{}, fmt.Errorf("dbdump: %q is not a MySQL-family engine", engine)
	}

	user := strings.TrimSpace(target.User)
	if user == "" {
		user = "root"
	}
	if err := validIdentifier(user); err != nil {
		return Result{}, fmt.Errorf("dbdump: refusing database user %q: %w", target.User, err)
	}

	relative := filepath.Join(dumpDirectory, fmt.Sprintf("%s-%s.sql", pathSegment(target.Service), engine))
	absolute := filepath.Join(stagingDir, relative)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump directory: %w", err)
	}

	// MYSQL_PWD is understood by both families and keeps the password out of
	// argv. Assembled here and handed straight to the daemon; it is never
	// logged, and the value is not part of anything this function returns.
	var env []string
	if target.Password != "" {
		env = append(env, "MYSQL_PWD="+target.Password)
	}

	// The databases to dump are enumerated first, so the system schema can be
	// left out by name. Without this the dump is unrestorable — see
	// systemSchemas.
	databases, err := listMySQLDatabases(ctx, client, target, engine, env)
	if err != nil {
		return Result{}, err
	}
	if len(databases) == 0 {
		return Result{}, fmt.Errorf("dbdump: %s holds no user databases to export", engine)
	}

	var lastErr error
	for _, tool := range tools {
		command := []string{tool,
			"--databases",
			// One consistent read view instead of locking the server.
			"--single-transaction",
			// Stored programs and scheduled events are part of the schema; a
			// dump without them restores a database that is missing behaviour.
			"--routines", "--triggers", "--events",
			"--user", user,
		}
		command = append(command, databases...)

		result, err := runSQLDump(ctx, client, target, command, env, absolute, relative, engine)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isMissingTool(err) {
			// A real failure — wrong password, unreachable server. Trying the
			// other binary would only produce the same error twice.
			return Result{}, err
		}
	}
	return Result{}, fmt.Errorf("dbdump: no dump tool found in the container (tried %s): %w",
		strings.Join(tools, ", "), lastErr)
}

// runSQLDump executes one dump command and writes its output.
func runSQLDump(
	ctx context.Context,
	client docker.Client,
	target Target,
	command, env []string,
	absolute, relative, technology string,
) (Result, error) {
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("dbdump: create dump file: %w", err)
	}
	defer file.Close()

	started := time.Now()
	run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
		Cmd:    command,
		Env:    env,
		Stdout: file,
	})
	if err != nil {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: run %s: %w", command[0], err)
	}
	if run.ExitCode != 0 {
		removeFailedDump(absolute)
		return Result{}, fmt.Errorf("dbdump: %s exited %d: %s",
			command[0], run.ExitCode, redactPassword(strings.TrimSpace(run.Stderr), env))
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
		return Result{}, fmt.Errorf("dbdump: %s produced an empty dump", command[0])
	}

	return Result{
		Technology: technology,
		Service:    target.Service,
		Path:       relative,
		Command:    strings.Join(command, " "),
		User:       firstNonEmpty(target.User, "root"),
		Bytes:      info.Size(),
		Duration:   time.Since(started),
	}, nil
}

// isMissingTool reports whether a failure means the binary is simply not in
// the image, which is the one case worth retrying with a different name.
func isMissingTool(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "executable file not found") ||
		strings.Contains(text, "no such file or directory") ||
		strings.Contains(text, "exited 127")
}

// redactPassword keeps a secret out of an error message.
//
// The tools echo their own invocation on some failures, and an error string
// travels into a run's warnings, the audit log and the UI. A password that
// reaches any of those has effectively been published.
func redactPassword(text string, env []string) string {
	for _, entry := range env {
		_, value, found := strings.Cut(entry, "=")
		if found && value != "" {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	return text
}

// listMySQLDatabases asks the server which databases it has, so the system
// schema can be excluded by name.
func listMySQLDatabases(
	ctx context.Context,
	client docker.Client,
	target Target,
	engine string,
	env []string,
) ([]string, error) {
	var lastErr error
	for _, tool := range mysqlClients[engine] {
		var out strings.Builder
		// -N drops the header, -B makes the output one name per line.
		run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
			Cmd:    []string{tool, "-N", "-B", "--user", firstNonEmpty(target.User, "root"), "-e", "SHOW DATABASES"},
			Env:    env,
			Stdout: &out,
		})
		if err != nil {
			lastErr = err
			if isMissingTool(err) {
				continue
			}
			return nil, fmt.Errorf("dbdump: list databases: %w", err)
		}
		if run.ExitCode != 0 {
			lastErr = fmt.Errorf("dbdump: %s exited %d: %s", tool, run.ExitCode,
				redactPassword(strings.TrimSpace(run.Stderr), env))
			if run.ExitCode == 127 {
				continue
			}
			return nil, lastErr
		}

		var databases []string
		for _, line := range strings.Split(out.String(), "\n") {
			name := strings.TrimSpace(line)
			if name == "" || systemSchemas[name] {
				continue
			}
			// The name goes back to the server as its own argument. Anything
			// that is not a plain identifier is refused rather than quoted,
			// because a database Back-Orbit cannot name safely is one it
			// should not silently skip either.
			if err := validIdentifier(name); err != nil {
				return nil, fmt.Errorf("dbdump: refusing database name %q: %w", name, err)
			}
			databases = append(databases, name)
		}
		return databases, nil
	}
	return nil, fmt.Errorf("dbdump: could not list databases: %w", lastErr)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
