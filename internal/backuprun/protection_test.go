package backuprun

import (
	"strings"
	"testing"
)

// TestReplayCommandIsWrittenForThePersonRestoring. A dump inside a snapshot is
// a file until someone knows what to do with it, and the person reading this
// is standing in front of a broken system.
func TestReplayCommandIsWrittenForThePersonRestoring(t *testing.T) {
	cases := map[string]struct {
		dump DatabaseDump
		want []string
	}{
		"postgresql": {
			dump: DatabaseDump{Technology: "postgresql", Service: "db", User: "app",
				Level: ProtectionExported, Path: "back-orbit-dumps/db-postgresql.sql"},
			want: []string{"docker compose exec", "db", "psql", "-U app", "db-postgresql.sql"},
		},
		"mysql": {
			dump: DatabaseDump{Technology: "mysql", Service: "shop", User: "root",
				Level: ProtectionExported, Path: "back-orbit-dumps/shop-mysql.sql"},
			want: []string{"docker compose exec", "shop", "mysql", "-u root", "-p", "shop-mysql.sql"},
		},
		"mariadb uses its own client": {
			dump: DatabaseDump{Technology: "mariadb", Service: "lager", User: "root",
				Level: ProtectionExported, Path: "back-orbit-dumps/lager-mariadb.sql"},
			want: []string{"mariadb -u root"},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			replay := testCase.dump.Replay()
			for _, fragment := range testCase.want {
				if !strings.Contains(replay, fragment) {
					t.Errorf("replay is missing %q: %s", fragment, replay)
				}
			}
		})
	}
}

// TestReplayNeverCarriesAPassword: the command is shown in the UI and copied
// into terminals and tickets. `-p` without a value makes the client prompt.
func TestReplayNeverCarriesAPassword(t *testing.T) {
	dump := DatabaseDump{Technology: "mysql", Service: "shop", User: "root",
		Level: ProtectionExported, Path: "d.sql"}

	replay := dump.Replay()
	if strings.Contains(replay, "-proot") || strings.Contains(replay, "MYSQL_PWD") {
		t.Fatalf("the replay command must prompt rather than carry a secret: %s", replay)
	}
	if !strings.Contains(replay, "-p ") && !strings.HasSuffix(strings.Split(replay, " <")[0], "-p") {
		t.Errorf("expected a prompting -p: %s", replay)
	}
}

// TestOnlyExportsOfferAReplay. A file-level copy has no command that puts it
// back, and offering one would imply a guarantee that does not exist.
func TestOnlyExportsOfferAReplay(t *testing.T) {
	for name, dump := range map[string]DatabaseDump{
		"files only": {Technology: "mongodb", Service: "db", Level: ProtectionFilesOnly},
		"sqlite":     {Technology: "sqlite", Service: "vol", Level: ProtectionConsistent, Path: "vol/app.db"},
		"no path":    {Technology: "postgresql", Service: "db", Level: ProtectionExported},
	} {
		t.Run(name, func(t *testing.T) {
			if replay := dump.Replay(); replay != "" {
				t.Errorf("expected no replay command, got %q", replay)
			}
		})
	}
}

// TestUnknownEnginesGetNoInventedCommand: a command that does not work is
// worse than none, because it will be run.
func TestUnknownEnginesGetNoInventedCommand(t *testing.T) {
	dump := DatabaseDump{Technology: "cockroachdb", Service: "db",
		Level: ProtectionExported, Path: "d.sql"}

	if replay := dump.Replay(); replay != "" {
		t.Errorf("got %q, want nothing for an engine with no known replay", replay)
	}
}

func TestUnconfiguredUsersFallBackToTheImageDefault(t *testing.T) {
	postgres := DatabaseDump{Technology: "postgresql", Service: "db",
		Level: ProtectionExported, Path: "d.sql"}
	if !strings.Contains(postgres.Replay(), "-U postgres") {
		t.Errorf("expected the PostgreSQL default user: %s", postgres.Replay())
	}

	mysql := DatabaseDump{Technology: "mysql", Service: "db",
		Level: ProtectionExported, Path: "d.sql"}
	if !strings.Contains(mysql.Replay(), "-u root") {
		t.Errorf("expected the MySQL default user: %s", mysql.Replay())
	}
}

// TestMongoReplayKeepsTheTargetsOwnAccounts. A mongodump archive contains the
// admin database, and restoring it replaces the target server's accounts with
// the source's. Verified against real servers: the documents came back, and
// then nothing could authenticate except the credentials from the machine the
// backup was taken on. mongodump has no --excludeDatabase, so the exclusion
// has to be on the way back in.
func TestMongoReplayKeepsTheTargetsOwnAccounts(t *testing.T) {
	dump := DatabaseDump{Technology: "mongodb", Service: "mongo", User: "admin",
		Level: ProtectionExported, Path: "back-orbit-dumps/mongo-mongodb.archive"}

	replay := dump.Replay()
	for _, needed := range []string{"mongorestore", "--archive", "--drop",
		"--nsExclude 'admin.*'", "--nsExclude 'config.*'"} {
		if !strings.Contains(replay, needed) {
			t.Errorf("replay is missing %q: %s", needed, replay)
		}
	}
	// -p ends the credentials, so the client prompts rather than carrying a
	// password into a terminal history.
	if !strings.Contains(replay, "-p <") {
		t.Errorf("the password must be prompted for: %s", replay)
	}
}

// TestAnUnauthenticatedMongoNeedsNoCredentialsInTheReplay either.
func TestAnUnauthenticatedMongoNeedsNoCredentialsInTheReplay(t *testing.T) {
	dump := DatabaseDump{Technology: "mongodb", Service: "mongo",
		Level: ProtectionExported, Path: "a.archive"}

	replay := dump.Replay()
	if strings.Contains(replay, "-u ") || strings.Contains(replay, "authenticationDatabase") {
		t.Errorf("an unauthenticated server needs no credentials: %s", replay)
	}
	if !strings.Contains(replay, "--nsExclude 'admin.*'") {
		t.Errorf("the exclusion applies regardless of authentication: %s", replay)
	}
}
