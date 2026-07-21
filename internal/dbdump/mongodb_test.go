package dbdump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mongoTarget() Target {
	return Target{Technology: "mongodb", Service: "mongo", ContainerID: "abc",
		User: "admin", Password: "geheim123"}
}

// TestMongoPasswordNeverReachesTheCommandLine is why this exporter takes the
// long way round. mongodump has no environment variable for a password, and
// argv is readable by any process on the host through `ps`.
func TestMongoPasswordNeverReachesTheCommandLine(t *testing.T) {
	client := fake("BSON-archive-bytes", 0, "")

	if _, err := MongoDB(context.Background(), client, mongoTarget(), t.TempDir()); err != nil {
		t.Fatalf("MongoDB: %v", err)
	}

	call := client.ExecCalls[0]
	for _, arg := range call.Cmd {
		if strings.Contains(arg, "geheim123") {
			t.Fatalf("the password appeared in argv: %v", call.Cmd)
		}
	}
	if !strings.Contains(string(call.Stdin), "geheim123") {
		t.Errorf("the password must be delivered on stdin, got %q", call.Stdin)
	}
	if !containsArg(call.Cmd, "/dev/stdin") {
		t.Errorf("mongodump must be told to read its config from stdin: %v", call.Cmd)
	}
}

// TestAnUnauthenticatedServerNeedsNoCredentials: the official image's default,
// and common inside a Compose network.
func TestAnUnauthenticatedServerNeedsNoCredentials(t *testing.T) {
	client := fake("BSON", 0, "")
	open := mongoTarget()
	open.User = ""
	open.Password = ""

	if _, err := MongoDB(context.Background(), client, open, t.TempDir()); err != nil {
		t.Fatalf("MongoDB: %v", err)
	}

	call := client.ExecCalls[0]
	if len(call.Stdin) != 0 {
		t.Errorf("no credentials means nothing on stdin, got %q", call.Stdin)
	}
	for _, flag := range []string{"--username", "--config"} {
		if containsArg(call.Cmd, flag) {
			t.Errorf("unexpected %s for an unauthenticated server: %v", flag, call.Cmd)
		}
	}
}

// TestAPasswordWithYAMLMetacharactersSurvives. The password is the one part of
// that config document Back-Orbit did not write; unquoted, a colon or a
// leading brace would be read as structure and the dump would fail in a way
// nobody could explain from the error.
func TestAPasswordWithYAMLMetacharactersSurvives(t *testing.T) {
	for _, password := range []string{"a:b", "{brace}", "with'quote", "#hash", "  spaced  "} {
		t.Run(password, func(t *testing.T) {
			client := fake("BSON", 0, "")
			awkward := mongoTarget()
			awkward.Password = password

			if _, err := MongoDB(context.Background(), client, awkward, t.TempDir()); err != nil {
				t.Fatalf("MongoDB: %v", err)
			}

			stdin := string(client.ExecCalls[0].Stdin)
			if !strings.HasPrefix(stdin, "password: '") || !strings.HasSuffix(stdin, "'\n") {
				t.Fatalf("the value must be quoted, got %q", stdin)
			}
		})
	}
}

// TestTheRecordedCommandDoesNotMentionStdin: how the password was delivered is
// an implementation detail, not part of what was dumped.
func TestTheRecordedCommandDoesNotMentionStdin(t *testing.T) {
	client := fake("BSON", 0, "")

	result, err := MongoDB(context.Background(), client, mongoTarget(), t.TempDir())
	if err != nil {
		t.Fatalf("MongoDB: %v", err)
	}
	if strings.Contains(result.Command, "/dev/stdin") || strings.Contains(result.Command, "--config") {
		t.Errorf("the recorded command should describe the dump, not the plumbing: %q", result.Command)
	}
	if !strings.Contains(result.Command, "--archive") {
		t.Errorf("expected --archive in %q", result.Command)
	}
}

func TestAFailedMongoDumpLeavesNothing(t *testing.T) {
	client := fake("partial", 1, "Failed: connection refused")
	staging := t.TempDir()

	if _, err := MongoDB(context.Background(), client, mongoTarget(), staging); err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	entries, _ := os.ReadDir(filepath.Join(staging, dumpDirectory))
	if len(entries) != 0 {
		t.Errorf("a failed dump left %d files behind", len(entries))
	}
}

func TestMongoErrorsDoNotCarryThePassword(t *testing.T) {
	client := fake("", 1, "Failed: auth error for user admin with password geheim123")

	_, err := MongoDB(context.Background(), client, mongoTarget(), t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "geheim123") {
		t.Fatalf("the password leaked into the error: %v", err)
	}
}
