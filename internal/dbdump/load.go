package dbdump

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// LoadResult describes what happened when a dump was put back.
type LoadResult struct {
	Technology string        `json:"technology"`
	Service    string        `json:"service"`
	Bytes      int64         `json:"bytes"`
	Duration   time.Duration `json:"-"`
	// Objects counts what is in the database after the load, where the engine
	// can be asked. It is the only evidence that a restore did anything.
	Objects int `json:"objects,omitempty"`
	// Output is whatever the tool said, bounded and redacted. Some loads
	// report recoverable problems this way and still succeed.
	Output string `json:"output,omitempty"`
}

// Load replays a dump into a running database, replacing what is there.
//
// This is the destructive half of a backup, and the only part of Back-Orbit
// that writes into a database someone is using. It exists because a dump that
// only ever gets shown as a command is a backup nobody has practised: the
// first real restore then happens under pressure, by hand, at the worst moment.
//
// The dump is read from a file the caller has already extracted from the
// snapshot. Nothing here reaches into a repository.
func Load(
	ctx context.Context,
	client docker.Client,
	target Target,
	dumpPath string,
) (LoadResult, error) {
	if target.ContainerID == "" {
		return LoadResult{}, fmt.Errorf("dbdump: %s is not running, so there is nothing to restore into", target.Service)
	}

	info, err := os.Stat(dumpPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: read the dump: %w", err)
	}
	if info.Size() == 0 {
		return LoadResult{}, fmt.Errorf("dbdump: the dump is empty; refusing to replace a live database with nothing")
	}

	started := time.Now()
	var result LoadResult

	switch target.Technology {
	case "postgresql":
		result, err = loadPostgres(ctx, client, target, dumpPath)
	case "mysql", "mariadb":
		clientName := "mysql"
		if target.Technology == "mariadb" {
			clientName = "mariadb"
		}
		var env []string
		if target.Password != "" {
			env = append(env, "MYSQL_PWD="+target.Password)
		}
		result, err = loadStreamed(ctx, client, target, dumpPath,
			[]string{clientName, "-u", defaultUser(target.User, "root")}, env)
	case "mongodb":
		result, err = loadMongo(ctx, client, target, dumpPath)
	default:
		return LoadResult{}, fmt.Errorf("dbdump: restoring %s is not supported yet", target.Technology)
	}
	if err != nil {
		return LoadResult{}, err
	}

	result.Technology = target.Technology
	result.Service = target.Service
	result.Bytes = info.Size()
	result.Duration = time.Since(started)
	return result, nil
}

// unavoidablePostgresErrors are the statements a cluster-wide dump cannot help
// failing when it is replayed into the live server it came from.
//
// pg_dumpall --clean drops everything before recreating it, including the two
// databases PostgreSQL will not let anyone drop (template1, and whichever one
// the session is connected to) and the role the session is connected as. None
// of them can be dropped, none of them needs to be, and each failed DROP is
// followed by a CREATE that fails because the object is still there. The
// contents are then restored on top, which is the outcome that was wanted.
var unavoidablePostgresErrors = []string{
	"cannot drop the currently open database",
	"current user cannot be dropped",
	"cannot be dropped because some objects depend on it",
	"is being accessed by other users",
	"already exists",
}

// loadPostgres replays a cluster-wide dump and then counts what came back.
//
// psql runs without ON_ERROR_STOP, and its exit code is deliberately not the
// verdict. Stopping at the first unavoidable failure above aborts the restore
// *after* the user databases have already been dropped — the worst outcome
// this code can produce, and exactly what the first live restore did: it
// destroyed the database it was asked to bring back.
//
// So the verdict is evidence instead. The tables are counted afterwards, the
// same way a backup's restore check proves a dump is loadable at all; a load
// that leaves nothing behind is reported as the failure it is.
func loadPostgres(ctx context.Context, client docker.Client, target Target, dumpPath string) (LoadResult, error) {
	user := defaultUser(target.User, "postgres")
	if err := validIdentifier(user); err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: refusing database user %q: %w", target.User, err)
	}

	// -d postgres: without it psql connects to a database named after the
	// user, which need not exist — and usually does not, when the image was
	// configured with POSTGRES_USER and POSTGRES_DB. The dump switches
	// database itself, so this is only where the session starts, and the
	// maintenance database "postgres" exists in every cluster initdb creates.
	result, err := loadStreamed(ctx, client, target, dumpPath,
		[]string{"psql", "-v", "ON_ERROR_STOP=0", "-U", user, "-d", "postgres"}, nil)
	if err != nil {
		return LoadResult{}, err
	}

	objects, err := countPostgresTablesAs(ctx, client, target.ContainerID, user, target.Password)
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: the load finished but its result could not be checked: %w", err)
	}
	if objects == 0 {
		return LoadResult{}, fmt.Errorf(
			"dbdump: the load left no tables behind, so the database was not restored: %s",
			firstLines(result.Output, 5))
	}

	result.Objects = objects
	result.Output = keepUnexpected(result.Output, unavoidablePostgresErrors)
	return result, nil
}

// keepUnexpected drops the lines a load is known to produce every time, so
// what remains is worth showing. Nothing is discarded on the strength of an
// exit code — only these named, understood messages.
func keepUnexpected(output string, known []string) string {
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		expected := false
		for _, pattern := range known {
			if strings.Contains(line, pattern) {
				expected = true
				break
			}
		}
		if !expected {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// loadStreamed pipes the dump straight into the client's standard input.
func loadStreamed(
	ctx context.Context,
	client docker.Client,
	target Target,
	dumpPath string,
	command, env []string,
) (LoadResult, error) {
	file, err := os.Open(dumpPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: open the dump: %w", err)
	}
	defer file.Close()

	run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
		Cmd:    command,
		Env:    env,
		Stdin:  file,
		Stdout: discard{},
	})
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: run %s: %w", command[0], err)
	}
	if run.ExitCode != 0 {
		return LoadResult{}, fmt.Errorf("dbdump: %s exited %d: %s", command[0], run.ExitCode,
			redactSecret(strings.TrimSpace(run.Stderr), target.Password))
	}
	return LoadResult{Output: redactSecret(strings.TrimSpace(run.Stderr), target.Password)}, nil
}

// mongoRestorePath is where the archive is placed inside the target container.
const mongoRestorePath = "/tmp/back-orbit-restore.archive"

// loadMongo uploads the archive and replays it.
//
// mongorestore reads its archive from standard input, which leaves nowhere to
// hand it a password — that also has to arrive on stdin as a config file. Only
// one of them can have it, so the archive goes in as a file and the credentials
// keep the pipe. The file is the database's own data on its way back into that
// database, and it is removed afterwards either way.
//
// The exclusions match the replay command exactly. Restoring the admin database
// would replace the target server's accounts with the ones from the machine the
// backup was taken on — verified, and the reason this is not a plain restore.
func loadMongo(ctx context.Context, client docker.Client, target Target, dumpPath string) (LoadResult, error) {
	archive, err := os.ReadFile(dumpPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: read the archive: %w", err)
	}

	bundle, err := tarSingleFile("back-orbit-restore.archive", archive)
	if err != nil {
		return LoadResult{}, err
	}
	if err := client.PutArchive(ctx, target.ContainerID, "/tmp", bundle); err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: place the archive in the container: %w", err)
	}
	defer func() {
		// Removed on every path: the archive is a full copy of the database
		// sitting in a container that outlives this call.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := client.ExecInContainer(cleanupCtx, target.ContainerID, docker.ExecRequest{
			Cmd:    []string{"rm", "-f", mongoRestorePath},
			Stdout: discard{},
		}); err != nil {
			slog.Error("dbdump: could not remove the uploaded archive from the container",
				"container", target.ContainerID, "path", mongoRestorePath, "error", err)
		}
	}()

	command := []string{"mongorestore", "--archive=" + mongoRestorePath, "--drop",
		"--nsExclude", "admin.*", "--nsExclude", "config.*"}
	var stdin io.Reader

	if user := strings.TrimSpace(target.User); user != "" {
		if err := validIdentifier(user); err != nil {
			return LoadResult{}, fmt.Errorf("dbdump: refusing database user %q: %w", target.User, err)
		}
		command = append(command, "--username", user, "--authenticationDatabase", "admin")
		if target.Password != "" {
			command = append(command, "--config", "/dev/stdin")
			stdin = strings.NewReader("password: " + quoteYAML(target.Password) + "\n")
		}
	}

	run, err := client.ExecInContainer(ctx, target.ContainerID, docker.ExecRequest{
		Cmd:    command,
		Stdin:  stdin,
		Stdout: discard{},
	})
	if err != nil {
		return LoadResult{}, fmt.Errorf("dbdump: run mongorestore: %w", err)
	}
	if run.ExitCode != 0 {
		return LoadResult{}, fmt.Errorf("dbdump: mongorestore exited %d: %s", run.ExitCode,
			redactSecret(strings.TrimSpace(run.Stderr), target.Password))
	}
	return LoadResult{Output: redactSecret(strings.TrimSpace(run.Stderr), target.Password)}, nil
}

// tarSingleFile wraps one file in the tar stream the upload API expects.
func tarSingleFile(name string, content []byte) (io.Reader, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)

	header := &tar.Header{
		Name: name,
		Mode: 0o600,
		Size: int64(len(content)),
	}
	if err := writer.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("dbdump: build upload: %w", err)
	}
	if _, err := writer.Write(content); err != nil {
		return nil, fmt.Errorf("dbdump: build upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("dbdump: build upload: %w", err)
	}
	return &buffer, nil
}

func defaultUser(configured, fallback string) string {
	if user := strings.TrimSpace(configured); user != "" {
		return user
	}
	return fallback
}
