package dbdump

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// scriptedClient answers each exec by matching the command, so a test can make
// the readiness probe succeed, the load fail, and the count return whatever it
// likes — the sequence a real verification walks through.
type scriptedClient struct {
	*docker.FakeClient
	loadExit    int
	loadStderr  string
	databases   string
	tableCounts map[string]string
	removed     []string
}

func (s *scriptedClient) ExecInContainer(ctx context.Context, id string, req docker.ExecRequest) (docker.ExecResult, error) {
	joined := strings.Join(req.Cmd, " ")

	switch {
	case strings.Contains(joined, "pg_isready"):
		return docker.ExecResult{ExitCode: 0}, nil

	case strings.Contains(joined, "pg_database"):
		_, _ = io.WriteString(req.Stdout.(io.Writer), s.databases)
		return docker.ExecResult{ExitCode: 0}, nil

	case strings.Contains(joined, "pg_tables"):
		// Matched on the -d argument alone. A looser match would also hit the
		// "-U postgres" in every command and answer for the wrong database.
		for database, count := range s.tableCounts {
			if strings.Contains(joined, "-d "+database+" ") {
				_, _ = io.WriteString(req.Stdout.(io.Writer), count)
				return docker.ExecResult{ExitCode: 0}, nil
			}
		}
		_, _ = io.WriteString(req.Stdout.(io.Writer), "0\n")
		return docker.ExecResult{ExitCode: 0}, nil

	default: // the load
		if req.Stdin != nil {
			_, _ = io.Copy(io.Discard, req.Stdin)
		}
		return docker.ExecResult{ExitCode: s.loadExit, Stderr: s.loadStderr}, nil
	}
}

func (s *scriptedClient) RemoveContainer(ctx context.Context, id string) error {
	s.removed = append(s.removed, id)
	return nil
}

func scripted(t *testing.T, loadExit int, stderr, databases string, counts map[string]string) (*scriptedClient, Result, string) {
	t.Helper()
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, dumpDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dumpDirectory, "db-postgresql.sql")
	if err := os.WriteFile(filepath.Join(staging, path), []byte("CREATE TABLE k();\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return &scriptedClient{
			FakeClient: docker.NewFakeClient(),
			loadExit:   loadExit, loadStderr: stderr,
			databases: databases, tableCounts: counts,
		},
		Result{Technology: "postgresql", Service: "db", Path: path, User: "app"},
		staging
}

// TestARestorableDumpIsReportedAsSuch counts across every restored database,
// not just the one the connection happens to land in — pg_tables is
// per-database, and counting in the wrong one reported a healthy dump as
// empty.
func TestARestorableDumpIsReportedAsSuch(t *testing.T) {
	client, dump, staging := scripted(t, 0, "", "postgres\nkunden\n",
		map[string]string{"postgres": "0\n", "kunden": "2\n"})

	check, err := VerifyRestorable(context.Background(), client, "postgres:17", dump, staging)
	if err != nil {
		t.Fatalf("VerifyRestorable: %v", err)
	}
	if !check.Loaded {
		t.Fatalf("expected a successful restore, got: %s", check.Detail)
	}
	if check.Objects != 2 {
		t.Errorf("Objects = %d, want 2 — the tables live in kunden, not in the default database", check.Objects)
	}
}

// TestADumpThatWillNotLoadIsCaught is the whole point. Both real bugs found
// while building the exporters produced a dump of plausible size that restored
// nothing, and neither was visible without replaying it.
func TestADumpThatWillNotLoadIsCaught(t *testing.T) {
	client, dump, staging := scripted(t, 1, "ERROR: syntax error at or near \"CREAT\"", "postgres\n", nil)

	check, err := VerifyRestorable(context.Background(), client, "postgres:17", dump, staging)
	if err != nil {
		t.Fatalf("a dump that fails to load is a result, not an engine error: %v", err)
	}
	if check.Loaded {
		t.Fatal("a dump that exits non-zero must not be reported as restorable")
	}
	if !strings.Contains(check.Detail, "syntax error") {
		t.Errorf("the reason must survive: %q", check.Detail)
	}
}

// TestADumpThatLoadsButRestoresNothingIsCaught: the quietest failure of all —
// every command succeeds and the server is empty.
func TestADumpThatLoadsButRestoresNothingIsCaught(t *testing.T) {
	client, dump, staging := scripted(t, 0, "", "postgres\n", map[string]string{"postgres": "0\n"})

	check, err := VerifyRestorable(context.Background(), client, "postgres:17", dump, staging)
	if err != nil {
		t.Fatalf("VerifyRestorable: %v", err)
	}
	if check.Loaded {
		t.Fatal("a load that produced no tables must not count as restorable")
	}
	if !strings.Contains(check.Detail, "no tables") {
		t.Errorf("the reason should say what was missing: %q", check.Detail)
	}
}

// TestTheThrowawayServerIsAlwaysRemoved. It holds a copy of the database, so
// leaking one leaks the data as well as the container.
func TestTheThrowawayServerIsAlwaysRemoved(t *testing.T) {
	for name, loadExit := range map[string]int{"after success": 0, "after failure": 1} {
		t.Run(name, func(t *testing.T) {
			client, dump, staging := scripted(t, loadExit, "", "postgres\n",
				map[string]string{"postgres": "1\n"})

			if _, err := VerifyRestorable(context.Background(), client, "postgres:17", dump, staging); err != nil {
				t.Fatalf("VerifyRestorable: %v", err)
			}
			if len(client.removed) == 0 {
				t.Error("the verification server was left running")
			}
		})
	}
}

// TestTheThrowawayServerIsIsolated: it holds a full copy of the database, so
// it must not be able to reach anything, and its data must disappear with it.
func TestTheThrowawayServerIsIsolated(t *testing.T) {
	client, dump, staging := scripted(t, 0, "", "postgres\n", map[string]string{"postgres": "1\n"})

	if _, err := VerifyRestorable(context.Background(), client, "postgres:17", dump, staging); err != nil {
		t.Fatalf("VerifyRestorable: %v", err)
	}

	created := client.CreatedContainers[0]
	if created.Source != "" || created.MountPath != "" {
		t.Errorf("the verification server must mount nothing, got %q at %q", created.Source, created.MountPath)
	}
	if !created.Server {
		t.Error("it has to run the image the way it normally runs")
	}
	// Its credentials are generated per run; nothing else ever connects to it.
	if len(created.Env) == 0 || !strings.HasPrefix(created.Env[0], "POSTGRES_PASSWORD=") {
		t.Errorf("expected generated credentials, got %v", created.Env)
	}
	if len(created.Env[0]) < len("POSTGRES_PASSWORD=")+32 {
		t.Errorf("the generated password looks too short: %d chars", len(created.Env[0]))
	}
}

func TestAnEngineWithoutARestoreCheckSaysSo(t *testing.T) {
	client, dump, staging := scripted(t, 0, "", "", nil)
	dump.Technology = "redis"

	if _, err := VerifyRestorable(context.Background(), client, "redis:7", dump, staging); err == nil {
		t.Fatal("an engine with no restore check must report that, not pass silently")
	}
}

// TestEveryExportedEngineCanBeChecked keeps the two lists from drifting apart.
// An exporter without a restore check is an export nobody can prove — which is
// the state this whole file exists to end.
func TestEveryExportedEngineCanBeChecked(t *testing.T) {
	// The engines Back-Orbit exports. SQLite is absent because it is captured
	// in place rather than exported to a file.
	for _, engine := range []string{"postgresql", "mysql", "mariadb", "mongodb"} {
		t.Run(engine, func(t *testing.T) {
			plan, known := restorePlans[engine]
			if !known {
				t.Fatalf("%s can be exported but not checked", engine)
			}
			if plan.env == nil || plan.ready == nil || plan.load == nil || plan.countObjects == nil {
				t.Errorf("%s has an incomplete restore plan", engine)
			}
		})
	}
}

// TestTheMongoCheckRepeatsTheReplaysExclusions. Checking a different restore
// than the one Back-Orbit tells people to run would verify the wrong thing —
// and the admin database is exactly what made a real restore replace the
// target's accounts.
func TestTheMongoCheckRepeatsTheReplaysExclusions(t *testing.T) {
	load := strings.Join(restorePlans["mongodb"].load("", ""), " ")

	for _, needed := range []string{"mongorestore", "--archive", "--drop", "admin.*", "config.*"} {
		if !strings.Contains(load, needed) {
			t.Errorf("the check's load is missing %q: %s", needed, load)
		}
	}
}

// TestThrowawayServersNeedNoSecrets: the MySQL family and MongoDB run without
// credentials here, which is better than generating a password and then having
// to keep it off argv.
func TestThrowawayServersNeedNoSecrets(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "mongodb"} {
		t.Run(engine, func(t *testing.T) {
			plan := restorePlans[engine]
			for _, cmd := range [][]string{plan.ready("", "pw"), plan.load("", "pw"), plan.env("pw")} {
				for _, part := range cmd {
					if strings.Contains(part, "pw") {
						t.Errorf("a secret reached %v", cmd)
					}
				}
			}
		})
	}
}
