package dbdump

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

func writeDump(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dump.sql")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	return path
}

func loadTarget(technology string) Target {
	return Target{
		Technology: technology, Service: "db", ContainerID: "abc",
		User: "root", Password: "s3cret",
	}
}

// TestLoadRefusesAnEmptyDump guards the worst outcome this package can
// produce: dropping a live database and putting nothing back.
func TestLoadRefusesAnEmptyDump(t *testing.T) {
	client := docker.NewFakeClient()

	_, err := Load(context.Background(), client, loadTarget("postgresql"), writeDump(t, ""))
	if err == nil {
		t.Fatal("an empty dump was accepted")
	}
	if len(client.ExecCalls) != 0 {
		t.Fatalf("the container was touched anyway: %v", client.ExecCalls)
	}
}

// postgresLoadFake answers the load, then the two catalogue queries that
// count what came back: the database list, then a table count per database.
type postgresLoadFake struct {
	*docker.FakeClient
	tables string
}

func (p *postgresLoadFake) ExecInContainer(ctx context.Context, id string, req docker.ExecRequest) (docker.ExecResult, error) {
	result, err := p.FakeClient.ExecInContainer(ctx, id, req)
	if err != nil {
		return result, err
	}
	for i, arg := range req.Cmd {
		if arg != "-c" || i+1 >= len(req.Cmd) {
			continue
		}
		if strings.Contains(req.Cmd[i+1], "pg_database") {
			_, _ = req.Stdout.Write([]byte("shop\n"))
		} else {
			_, _ = req.Stdout.Write([]byte(p.tables + "\n"))
		}
	}
	return result, nil
}

func newPostgresLoadFake(tables string) *postgresLoadFake {
	return &postgresLoadFake{FakeClient: docker.NewFakeClient(), tables: tables}
}

// TestLoadStreamsTheDumpOnStdin proves the dump never becomes an argument,
// and that the whole file reaches the client.
func TestLoadStreamsTheDumpOnStdin(t *testing.T) {
	const dump = "CREATE DATABASE shop;\nINSERT INTO t VALUES (1);\n"
	client := newPostgresLoadFake("4")

	result, err := Load(context.Background(), client, loadTarget("postgresql"), writeDump(t, dump))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// One load, then the catalogue queries that count what came back.
	if len(client.ExecCalls) != 3 {
		t.Fatalf("expected a load and two catalogue queries, got %d calls", len(client.ExecCalls))
	}
	if got := string(client.ExecStdin[0]); got != dump {
		t.Fatalf("the client received %q, not the dump", got)
	}
	if result.Bytes != int64(len(dump)) {
		t.Fatalf("reported %d bytes for a %d byte dump", result.Bytes, len(dump))
	}
	if result.Objects != 4 {
		t.Fatalf("the restore reported %d tables, not the 4 it counted", result.Objects)
	}
	// Without an explicit database psql connects to one named after the user,
	// which need not exist — the failure the first live restore hit.
	cmd := client.ExecCalls[0].Cmd
	if cmd[0] != "psql" || !contains(cmd, "-d") || !contains(cmd, "postgres") {
		t.Fatalf("psql was given no database to connect to: %v", cmd)
	}
}

// TestAPostgresLoadThatLeavesNothingIsAFailure.
//
// A cluster-wide dump always fails a few statements it cannot help — dropping
// the open database, the connected role, template1 — so the exit code cannot
// be the verdict. Stopping on those aborts the restore after the databases
// have already been dropped, which is how the first live restore destroyed the
// database it was asked to bring back. The evidence is what came back instead.
func TestAPostgresLoadThatLeavesNothingIsAFailure(t *testing.T) {
	client := newPostgresLoadFake("0")

	_, err := Load(context.Background(), client, loadTarget("postgresql"),
		writeDump(t, "DROP DATABASE shop;\n"))
	if err == nil {
		t.Fatal("a load that restored nothing was reported as a success")
	}
	if !strings.Contains(err.Error(), "no tables") {
		t.Fatalf("the error does not say what went wrong: %v", err)
	}
}

// TestUnavoidablePostgresErrorsAreNotReportedAsProblems, because a warning
// that appears on every healthy restore teaches people to ignore warnings.
func TestUnavoidablePostgresErrorsAreNotReportedAsProblems(t *testing.T) {
	client := newPostgresLoadFake("4")
	client.ExecResult = docker.ExecResult{Stderr: strings.Join([]string{
		`ERROR:  cannot drop the currently open database`,
		`ERROR:  current user cannot be dropped`,
		`ERROR:  database "template1" already exists`,
		`ERROR:  permission denied for schema public`,
	}, "\n")}

	result, err := Load(context.Background(), client, loadTarget("postgresql"),
		writeDump(t, "SELECT 1;\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(result.Output, "currently open database") {
		t.Fatalf("an unavoidable message was reported as a problem: %q", result.Output)
	}
	// Everything not on the known list still has to reach the operator.
	if !strings.Contains(result.Output, "permission denied for schema public") {
		t.Fatalf("a real error was filtered away: %q", result.Output)
	}
}

// TestMySQLLoadKeepsThePasswordOutOfArgv mirrors the export side: argv is
// readable by every process on the host.
func TestMySQLLoadKeepsThePasswordOutOfArgv(t *testing.T) {
	client := docker.NewFakeClient()

	if _, err := Load(context.Background(), client, loadTarget("mariadb"),
		writeDump(t, "USE shop;\n")); err != nil {
		t.Fatalf("Load: %v", err)
	}

	call := client.ExecCalls[0]
	for _, arg := range call.Cmd {
		if strings.Contains(arg, "s3cret") {
			t.Fatalf("the password appeared in argv: %v", call.Cmd)
		}
	}
	if !contains(call.Env, "MYSQL_PWD=s3cret") {
		t.Fatalf("the password did not reach the client at all: %v", call.Env)
	}
	if call.Cmd[0] != "mariadb" {
		t.Fatalf("mariadb was loaded with %q", call.Cmd[0])
	}
}

// TestLoadFailsLoudlyOnANonZeroExit: a half-applied dump reported as success
// is how a restore silently leaves a database in an invented state.
func TestLoadFailsLoudlyOnANonZeroExit(t *testing.T) {
	client := docker.NewFakeClient()
	client.ExecResult = docker.ExecResult{ExitCode: 1, Stderr: "ERROR: connection refused"}

	_, err := Load(context.Background(), client, loadTarget("postgresql"), writeDump(t, "SELECT 1;\n"))
	if err == nil {
		t.Fatal("a failed import was reported as a success")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("the error lost what the tool said: %v", err)
	}
}

// TestMongoUploadsTheArchiveAndRemovesIt: the archive is a full copy of the
// database, left inside a container that outlives the restore.
func TestMongoUploadsTheArchiveAndRemovesIt(t *testing.T) {
	const archive = "\x00\x01binary archive body"
	client := docker.NewFakeClient()

	if _, err := Load(context.Background(), client, loadTarget("mongodb"),
		writeDump(t, archive)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(client.Uploads) != 1 {
		t.Fatalf("expected one upload, got %d", len(client.Uploads))
	}
	if body := untarSingle(t, client.Uploads[0].Bytes); body != archive {
		t.Fatalf("the uploaded archive was not the dump: %q", body)
	}

	var restored, removed bool
	for _, call := range client.ExecCalls {
		switch call.Cmd[0] {
		case "mongorestore":
			restored = true
			if !contains(call.Cmd, "admin.*") {
				t.Fatalf("the target's own accounts were not excluded: %v", call.Cmd)
			}
			for _, arg := range call.Cmd {
				if strings.Contains(arg, "s3cret") {
					t.Fatalf("the password appeared in argv: %v", call.Cmd)
				}
			}
		case "rm":
			removed = true
			if !contains(call.Cmd, mongoRestorePath) {
				t.Fatalf("something other than the archive was removed: %v", call.Cmd)
			}
		}
	}
	if !restored {
		t.Fatal("mongorestore never ran")
	}
	if !removed {
		t.Fatal("the uploaded archive was left behind in the container")
	}
}

// TestUnsupportedTechnologySaysSo rather than silently doing nothing.
func TestUnsupportedTechnologySaysSo(t *testing.T) {
	client := docker.NewFakeClient()

	_, err := Load(context.Background(), client, loadTarget("redis"), writeDump(t, "x"))
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected a clear refusal, got %v", err)
	}
	if len(client.ExecCalls) != 0 {
		t.Fatalf("the container was touched anyway: %v", client.ExecCalls)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func untarSingle(t *testing.T, bundle []byte) string {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(bundle))
	if _, err := reader.Next(); err != nil {
		t.Fatalf("read upload: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read upload body: %v", err)
	}
	return string(body)
}
