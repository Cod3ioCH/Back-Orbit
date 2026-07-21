package dbdump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// mysqlFake answers the database listing first, then the dump — the two calls
// the exporter now makes.
type mysqlFake struct {
	*docker.FakeClient
	listing string
	dump    string
	code    int
	stderr  string
	calls   int
}

func (m *mysqlFake) ExecInContainer(ctx context.Context, id string, req docker.ExecRequest) (docker.ExecResult, error) {
	m.calls++
	m.FakeClient.ExecCalls = append(m.FakeClient.ExecCalls, req)
	if m.calls == 1 {
		_, _ = req.Stdout.Write([]byte(m.listing))
		return docker.ExecResult{}, nil
	}
	_, _ = req.Stdout.Write([]byte(m.dump))
	return docker.ExecResult{ExitCode: m.code, Stderr: m.stderr}, nil
}

func newMySQLFake(dump string, code int, stderr string) *mysqlFake {
	return &mysqlFake{
		FakeClient: docker.NewFakeClient(),
		listing:    "information_schema\nmysql\nperformance_schema\nsys\nshop\n",
		dump:       dump, code: code, stderr: stderr,
	}
}

func mysqlTarget(engine string) Target {
	return Target{Technology: engine, Service: "db", ContainerID: "abc", User: "root", Password: "s3cret"}
}

// TestPasswordNeverReachesTheCommandLine is the property that matters most
// here: argv is readable by any process on the host through `ps`.
func TestPasswordNeverReachesTheCommandLine(t *testing.T) {
	client := newMySQLFake("-- MySQL dump\nCREATE DATABASE app;\n", 0, "")

	if _, err := MySQL(context.Background(), client, mysqlTarget("mysql"), t.TempDir()); err != nil {
		t.Fatalf("MySQL: %v", err)
	}

	call := client.ExecCalls[1]
	for _, arg := range call.Cmd {
		if strings.Contains(arg, "s3cret") {
			t.Fatalf("the password appeared in argv: %v", call.Cmd)
		}
	}
	found := false
	for _, entry := range call.Env {
		if entry == "MYSQL_PWD=s3cret" {
			found = true
		}
	}
	if !found {
		t.Errorf("the password must be passed through the environment, got %v", call.Env)
	}
}

// TestConsistencyFlagsArePresent: without --single-transaction the dump either
// locks the application out or captures a moving target.
func TestConsistencyFlagsArePresent(t *testing.T) {
	client := newMySQLFake("-- dump\n", 0, "")
	if _, err := MySQL(context.Background(), client, mysqlTarget("mariadb"), t.TempDir()); err != nil {
		t.Fatalf("MySQL: %v", err)
	}

	cmd := strings.Join(client.ExecCalls[1].Cmd, " ")
	for _, flag := range []string{"--databases", "--single-transaction", "--routines", "--triggers", "--events"} {
		if !strings.Contains(cmd, flag) {
			t.Errorf("missing %s in %q", flag, cmd)
		}
	}
}

// TestToolNameDiffersByEngine: MariaDB 11 removed mysqldump entirely and MySQL
// 8 never had mariadb-dump. Picking one name would fail on half the
// installations in the wild.
func TestToolNameDiffersByEngine(t *testing.T) {
	for engine, want := range map[string]string{"mysql": "mysqldump", "mariadb": "mariadb-dump"} {
		t.Run(engine, func(t *testing.T) {
			client := newMySQLFake("-- dump\n", 0, "")
			if _, err := MySQL(context.Background(), client, mysqlTarget(engine), t.TempDir()); err != nil {
				t.Fatalf("MySQL: %v", err)
			}
			if got := client.ExecCalls[1].Cmd[0]; got != want {
				t.Errorf("tool = %q, want %q", got, want)
			}
		})
	}
}

// TestAFailedMySQLDumpLeavesNothing mirrors the PostgreSQL guarantee.
func TestAFailedMySQLDumpLeavesNothing(t *testing.T) {
	client := newMySQLFake("partial", 1, "Access denied for user")
	staging := t.TempDir()

	if _, err := MySQL(context.Background(), client, mysqlTarget("mysql"), staging); err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	entries, _ := os.ReadDir(filepath.Join(staging, dumpDirectory))
	if len(entries) != 0 {
		t.Errorf("a failed dump left %d files behind", len(entries))
	}
}

// TestErrorsDoNotCarryThePassword: an error string travels into the run's
// warnings, the audit log and the UI. A password reaching any of those has
// effectively been published.
func TestErrorsDoNotCarryThePassword(t *testing.T) {
	client := newMySQLFake("x", 1, "mysqldump: connect failed using password s3cret")

	_, err := MySQL(context.Background(), client, mysqlTarget("mysql"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("the password leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("the redaction should be visible: %v", err)
	}
}

var _ = docker.ExecRequest{}

// TestSystemSchemasAreNeverDumped is the bug that only a real restore
// revealed. --all-databases includes the `mysql` schema, whose user table
// carries the source server's credentials. Replaying it into a running server
// replaces its accounts mid-import, the session loses its authorisation, and
// the import dies before reaching any user database — because `mysql` sorts
// ahead of most names. The dump was 3.8 MB and restored nothing.
func TestSystemSchemasAreNeverDumped(t *testing.T) {
	client := newMySQLFake("-- dump\n", 0, "")

	if _, err := MySQL(context.Background(), client, mysqlTarget("mysql"), t.TempDir()); err != nil {
		t.Fatalf("MySQL: %v", err)
	}

	cmd := client.ExecCalls[1].Cmd
	if containsArg(cmd, "--all-databases") {
		t.Error("--all-databases drags in the system schema and makes the dump unrestorable")
	}
	for _, system := range []string{"mysql", "information_schema", "performance_schema", "sys"} {
		if containsArg(cmd, system) {
			t.Errorf("system schema %q must not be dumped: %v", system, cmd)
		}
	}
	if !containsArg(cmd, "shop") {
		t.Errorf("the user database must be dumped: %v", cmd)
	}
}

// TestAServerWithNoUserDatabasesIsReported: an export of nothing is not a
// successful export.
func TestAServerWithNoUserDatabasesIsReported(t *testing.T) {
	client := newMySQLFake("-- dump\n", 0, "")
	client.listing = "information_schema\nmysql\nperformance_schema\nsys\n"

	if _, err := MySQL(context.Background(), client, mysqlTarget("mysql"), t.TempDir()); err == nil {
		t.Fatal("a server with only system schemas must be reported, not exported as empty")
	}
}

// TestHostileDatabaseNamesAreRefused: names come back from the server and go
// out again as arguments.
func TestHostileDatabaseNamesAreRefused(t *testing.T) {
	client := newMySQLFake("-- dump\n", 0, "")
	client.listing = "shop\n--some-option\n"

	if _, err := MySQL(context.Background(), client, mysqlTarget("mysql"), t.TempDir()); err == nil {
		t.Fatal("a database name that would be read as an option must be refused")
	}
}
