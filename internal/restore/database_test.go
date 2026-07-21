package restore

import (
	"context"
	"errors"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

func databaseRunner(databases ...backuprun.DatabaseDump) *Runner {
	return &Runner{
		docker: docker.NewFakeClient(),
		snapshots: fakeSnapshots{&backuprun.Snapshot{
			ID: "snapshot-1",
			Manifest: backuprun.Manifest{
				Project:   "notes",
				Databases: databases,
			},
		}},
	}
}

func exported(service string) backuprun.DatabaseDump {
	return backuprun.DatabaseDump{
		Service: service, Technology: "postgresql",
		Level: backuprun.ProtectionExported, Path: "/staging/notes/" + service + ".sql",
	}
}

// TestTheConfirmationIsEnforcedOnTheServer. The typed name is a safeguard only
// if it is checked where the database is actually replaced — a client that
// calls the API directly never sees the dialog.
func TestTheConfirmationIsEnforcedOnTheServer(t *testing.T) {
	runner := databaseRunner(exported("db"))

	for _, confirm := range []string{"", "DB", "db ", "yes"} {
		_, err := runner.RestoreDatabase(context.Background(), DatabaseRequest{
			SnapshotID: "snapshot-1", Service: "db", Confirm: confirm,
		})
		if !errors.Is(err, ErrNotConfirmed) {
			t.Fatalf("confirm %q was accepted as naming the service: %v", confirm, err)
		}
	}
}

// TestFilesOnlyDatabasesAreNotOfferedForReplay: a database that was merely
// copied as files has no command that puts it back, and pretending otherwise
// would promise a restore this path cannot perform.
func TestFilesOnlyDatabasesAreNotOfferedForReplay(t *testing.T) {
	for _, level := range []backuprun.ProtectionLevel{backuprun.ProtectionFilesOnly, backuprun.ProtectionConsistent} {
		runner := databaseRunner(backuprun.DatabaseDump{
			Service: "db", Technology: "postgresql", Level: level,
			Path: "/staging/notes/db.sql",
		})

		_, err := runner.RestoreDatabase(context.Background(), DatabaseRequest{
			SnapshotID: "snapshot-1", Service: "db", Confirm: "db",
		})
		if !errors.Is(err, ErrNoExport) {
			t.Fatalf("level %s was treated as replayable: %v", level, err)
		}
	}
}

// TestAnUnknownServiceIsRejectedBeforeAnythingRuns.
func TestAnUnknownServiceIsRejectedBeforeAnythingRuns(t *testing.T) {
	runner := databaseRunner(exported("db"))

	_, err := runner.RestoreDatabase(context.Background(), DatabaseRequest{
		SnapshotID: "snapshot-1", Service: "cache", Confirm: "cache",
	})
	if !errors.Is(err, ErrNoExport) {
		t.Fatalf("a service the snapshot never captured was accepted: %v", err)
	}
}

// TestCredentialsAreReadOnlyFromTheEnginesOwnKeys. A container's environment
// holds more than one secret; a restore reads the few it needs and no others.
func TestCredentialsAreReadOnlyFromTheEnginesOwnKeys(t *testing.T) {
	client := docker.NewFakeClient()
	client.EnvValues = map[string]string{
		"POSTGRES_USER":              "app",
		"MYSQL_ROOT_PASSWORD":        "mysql-secret",
		"MONGO_INITDB_ROOT_PASSWORD": "mongo-secret",
		"STRIPE_API_KEY":             "sk_live_should_never_be_read",
	}
	runner := &Runner{docker: client}

	user, password := runner.credentialsFor(context.Background(), "abc", "postgresql")
	if user != "app" {
		t.Fatalf("expected the configured superuser, got %q", user)
	}
	// PostgreSQL loads over its local socket, where the server trusts its own
	// operating-system user, so no password is fetched at all.
	if password != "" {
		t.Fatalf("a password was fetched for a local-socket load: %q", password)
	}

	for _, call := range client.ExecCalls {
		t.Fatalf("reading credentials executed something: %v", call.Cmd)
	}
}

// TestAnUnsupportedEngineFetchesNothing.
func TestAnUnsupportedEngineFetchesNothing(t *testing.T) {
	client := docker.NewFakeClient()
	client.EnvValues = map[string]string{"REDIS_PASSWORD": "secret"}
	runner := &Runner{docker: client}

	if user, password := runner.credentialsFor(context.Background(), "abc", "redis"); user != "" || password != "" {
		t.Fatalf("credentials were invented for an unsupported engine: %q/%q", user, password)
	}
}
