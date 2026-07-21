package dbdump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

func fake(stdout string, exitCode int, stderr string) *docker.FakeClient {
	client := docker.NewFakeClient()
	client.ExecStdout = []byte(stdout)
	client.ExecResult = docker.ExecResult{ExitCode: exitCode, Stderr: stderr}
	return client
}

func target() Target {
	return Target{Technology: "postgresql", Service: "db", ContainerID: "abc123", User: "app"}
}

func TestDumpIsWrittenAndDescribed(t *testing.T) {
	client := fake("--\n-- PostgreSQL database cluster dump\n--\nCREATE ROLE app;\n", 0, "")
	staging := t.TempDir()

	result, err := PostgreSQL(context.Background(), client, target(), staging)
	if err != nil {
		t.Fatalf("PostgreSQL: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(staging, result.Path))
	if err != nil {
		t.Fatalf("the dump was not written where it says: %v", err)
	}
	if !strings.Contains(string(contents), "CREATE ROLE app") {
		t.Errorf("the dump does not hold what the database produced: %q", contents)
	}
	if result.Bytes != int64(len(contents)) {
		t.Errorf("Bytes = %d, want %d", result.Bytes, len(contents))
	}

	// A restore has to know how to read the file back without guessing.
	if !strings.Contains(result.Command, "pg_dumpall") {
		t.Errorf("the command that produced the dump must be recorded, got %q", result.Command)
	}

	// Roles and grants live outside any single database. A dump that restores
	// tables into a server with no users to own them is not a restore.
	call := client.ExecCalls[0]
	if call.Cmd[0] != "pg_dumpall" {
		t.Errorf("cmd = %v, want pg_dumpall", call.Cmd)
	}
	// An argument vector, never a shell string.
	if len(call.Cmd) < 2 {
		t.Fatalf("expected arguments, got %v", call.Cmd)
	}
	if !containsArg(call.Cmd, "app") {
		t.Errorf("the configured database user must be used, got %v", call.Cmd)
	}
}

// TestAFailedDumpLeavesNothingBehind: a partial file travelling into a
// snapshot as a successful export is worse than no export at all.
func TestAFailedDumpLeavesNothingBehind(t *testing.T) {
	client := fake("BEGIN;\n", 1, "connection refused")
	staging := t.TempDir()

	_, err := PostgreSQL(context.Background(), client, target(), staging)
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the reason must survive: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(staging, dumpDirectory))
	if len(entries) != 0 {
		t.Errorf("a failed dump left %d files behind", len(entries))
	}
}

// TestAnEmptyDumpIsRefused: pg_dumpall can exit zero having written nothing.
// Recording that as a successful export is the quietest possible failure.
func TestAnEmptyDumpIsRefused(t *testing.T) {
	staging := t.TempDir()

	if _, err := PostgreSQL(context.Background(), fake("", 0, ""), target(), staging); err == nil {
		t.Fatal("an empty dump must not be reported as a successful export")
	}
	entries, _ := os.ReadDir(filepath.Join(staging, dumpDirectory))
	if len(entries) != 0 {
		t.Errorf("an empty dump was left behind: %d files", len(entries))
	}
}

// TestHostileUserNamesAreRefused. The value comes from a container's
// environment, which comes from a Compose file. It is passed as its own
// argument and never through a shell, so this is a second line of defence —
// but a name that would be read as an option is worth refusing outright.
func TestHostileUserNamesAreRefused(t *testing.T) {
	for _, user := range []string{"--help", "-c", "app;rm -rf /", "app\nDROP", "app`id`"} {
		t.Run(user, func(t *testing.T) {
			hostile := target()
			hostile.User = user

			if _, err := PostgreSQL(context.Background(), fake("x", 0, ""), hostile, t.TempDir()); err == nil {
				t.Fatalf("user %q must be refused", user)
			}
		})
	}
}

func TestAnUnconfiguredContainerUsesTheImageDefault(t *testing.T) {
	client := fake("CREATE ROLE postgres;\n", 0, "")
	unset := target()
	unset.User = ""

	if _, err := PostgreSQL(context.Background(), client, unset, t.TempDir()); err != nil {
		t.Fatalf("PostgreSQL: %v", err)
	}
	if !containsArg(client.ExecCalls[0].Cmd, "postgres") {
		t.Errorf("expected the image default user, got %v", client.ExecCalls[0].Cmd)
	}
}

func TestARunningContainerIsRequired(t *testing.T) {
	stopped := target()
	stopped.ContainerID = ""

	if _, err := PostgreSQL(context.Background(), fake("x", 0, ""), stopped, t.TempDir()); err == nil {
		t.Fatal("a dump needs a container to run in")
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
