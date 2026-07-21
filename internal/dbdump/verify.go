package dbdump

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// RestoreCheck reports whether a dump could actually be loaded back.
type RestoreCheck struct {
	// Loaded is true when the dump replayed into an empty server without
	// error.
	Loaded bool `json:"loaded"`
	// Objects is how many tables or collections the restored server ended up
	// with. Zero after a load that reported success is its own kind of
	// failure.
	Objects int `json:"objects"`
	// Detail explains a failure in terms someone can act on.
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

// readyTimeout bounds how long a throwaway server gets to come up.
//
// Database images initialise on first start — creating a cluster, running
// entrypoint scripts — and that is slower than a plain container start.
const readyTimeout = 3 * time.Minute

// VerifyRestorable loads a dump into a throwaway server of the same image and
// reports whether it came back.
//
// This exists because a dump that cannot be restored looks exactly like one
// that can. Twice while building the exporters, an export completed, reported
// success, sat in a snapshot at a plausible size — and restored nothing:
// MySQL's dump carried the system schema and killed the importing session,
// MongoDB's replaced the target's accounts. Neither was visible from the dump
// itself. Only replaying it found them, and a check that depends on someone
// remembering to replay it by hand is not a check.
//
// The throwaway server is isolated: no network, no volumes, its own generated
// credentials, and its data lives in the container layer that disappears with
// it. It is created from the same image as the source, which is already
// present locally, so nothing is pulled.
func VerifyRestorable(
	ctx context.Context,
	client docker.Client,
	image string,
	dump Result,
	stagingDir string,
) (RestoreCheck, error) {
	if image == "" {
		return RestoreCheck{}, fmt.Errorf("dbdump: no image to verify %s against", dump.Technology)
	}
	plan, known := restorePlans[dump.Technology]
	if !known {
		return RestoreCheck{}, fmt.Errorf("dbdump: no restore check for %s", dump.Technology)
	}

	// Generated per run and never stored: this server exists for a few seconds
	// and nothing else will ever connect to it.
	password, err := throwawayPassword()
	if err != nil {
		return RestoreCheck{}, err
	}

	started := time.Now()
	containerID, err := client.CreateHelperContainer(ctx, docker.HelperContainerRequest{
		Image:   image,
		Server:  true,
		Env:     plan.env(password),
		Purpose: "verify-restore:" + dump.Technology + ":" + dump.Service,
	})
	if err != nil {
		return RestoreCheck{}, fmt.Errorf("dbdump: create verification server: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		if removeErr := client.RemoveContainer(cleanupCtx, containerID); removeErr != nil {
			slog.Error("dbdump: could not remove the verification server; it will be swept on next start",
				"container", containerID, "error", removeErr)
		}
	}()

	if err := client.StartContainer(ctx, containerID); err != nil {
		return RestoreCheck{}, fmt.Errorf("dbdump: start verification server: %w", err)
	}

	if err := waitReady(ctx, client, containerID, plan, password); err != nil {
		return RestoreCheck{Loaded: false, Detail: err.Error(),
			DurationMS: time.Since(started).Milliseconds()}, nil
	}

	file, err := os.Open(dump.Path)
	if err != nil {
		// Path is relative to the staged tree.
		file, err = os.Open(stagingDir + "/" + dump.Path)
		if err != nil {
			return RestoreCheck{}, fmt.Errorf("dbdump: read the dump to verify: %w", err)
		}
	}
	defer file.Close()

	load, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{
		Cmd:    plan.load(dump.User, password),
		Stdin:  file,
		Stdout: discard{},
	})
	if err != nil {
		return RestoreCheck{Loaded: false, Detail: err.Error(),
			DurationMS: time.Since(started).Milliseconds()}, nil
	}
	if load.ExitCode != 0 {
		return RestoreCheck{
			Loaded:     false,
			Detail:     redactSecret(strings.TrimSpace(load.Stderr), password),
			DurationMS: time.Since(started).Milliseconds(),
		}, nil
	}

	objects, err := plan.countObjects(ctx, client, containerID, password)
	if err != nil {
		return RestoreCheck{Loaded: true, Detail: err.Error(),
			DurationMS: time.Since(started).Milliseconds()}, nil
	}

	check := RestoreCheck{Loaded: true, Objects: objects,
		DurationMS: time.Since(started).Milliseconds()}
	if objects == 0 {
		// The load reported success and produced nothing. That is the quietest
		// failure of all, and exactly what this check exists to catch.
		check.Loaded = false
		check.Detail = "the dump replayed without error but the restored server holds no tables or collections"
	}
	return check, nil
}

// restorePlan is what one engine needs to be started, loaded and counted.
type restorePlan struct {
	env   func(password string) []string
	ready func(user, password string) []string
	load  func(user, password string) []string
	// countObjects walks the restored server and counts what came back.
	//
	// A function rather than a single command, because the answer is not one
	// query: pg_tables is per-database, so counting in the connection's own
	// database reports zero while the restored tables sit in another one. That
	// mistake made the check fail a dump that was perfectly good — and a check
	// that cries wolf teaches people to ignore it.
	countObjects func(ctx context.Context, client docker.Client, containerID, password string) (int, error)
}

var restorePlans = map[string]restorePlan{
	"postgresql": {
		env: func(password string) []string {
			return []string{"POSTGRES_PASSWORD=" + password}
		},
		ready: func(user, password string) []string {
			return []string{"pg_isready", "-U", "postgres"}
		},
		load: func(user, password string) []string {
			// As the image's own superuser: the dump creates the roles it
			// needs, including the one the source used.
			return []string{"psql", "-v", "ON_ERROR_STOP=0", "-U", "postgres"}
		},
		countObjects: countPostgresTables,
	},

	// The MySQL family starts with an empty root password. No secret is needed
	// anywhere in the check, which is better than generating one and having to
	// keep it off argv.
	"mysql": {
		env: func(string) []string { return []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"} },
		ready: func(string, string) []string {
			return []string{"mysqladmin", "ping", "-uroot"}
		},
		load:         func(string, string) []string { return []string{"mysql", "-uroot"} },
		countObjects: countMySQLTables("mysql"),
	},
	"mariadb": {
		env: func(string) []string { return []string{"MARIADB_ALLOW_EMPTY_ROOT_PASSWORD=yes"} },
		ready: func(string, string) []string {
			return []string{"mariadb-admin", "ping", "-uroot"}
		},
		load:         func(string, string) []string { return []string{"mariadb", "-uroot"} },
		countObjects: countMySQLTables("mariadb"),
	},

	// MongoDB runs unauthenticated here, and the load repeats the exclusions
	// the replay command carries. Checking a different restore than the one
	// Back-Orbit tells people to run would verify the wrong thing — and the
	// admin database is exactly what made a real restore replace the target's
	// accounts.
	"mongodb": {
		env: func(string) []string { return nil },
		ready: func(string, string) []string {
			return []string{"mongosh", "--quiet", "--eval", "db.runCommand({ping:1}).ok"}
		},
		load: func(string, string) []string {
			return []string{"mongorestore", "--archive", "--drop",
				"--nsExclude", "admin.*", "--nsExclude", "config.*"}
		},
		countObjects: countMongoCollections,
	},
}

// countMySQLTables counts user tables, excluding the system schemas that a
// dump must never carry.
func countMySQLTables(engine string) func(context.Context, docker.Client, string, string) (int, error) {
	client := "mysql"
	if engine == "mariadb" {
		client = "mariadb"
	}
	return func(ctx context.Context, docker_ docker.Client, containerID, password string) (int, error) {
		return countSingleNumber(ctx, docker_, containerID, password, []string{
			client, "-uroot", "-N", "-B", "-e",
			"select count(*) from information_schema.tables " +
				"where table_schema not in ('mysql','information_schema','performance_schema','sys')",
		})
	}
}

// countMongoCollections counts collections outside the system databases.
func countMongoCollections(ctx context.Context, client docker.Client, containerID, password string) (int, error) {
	return countSingleNumber(ctx, client, containerID, password, []string{
		"mongosh", "--quiet", "--eval",
		`db.adminCommand({listDatabases:1}).databases` +
			`.filter(d=>!["admin","config","local"].includes(d.name))` +
			`.reduce((n,d)=>n+db.getSiblingDB(d.name).getCollectionNames().length,0)`,
	})
}

// countSingleNumber runs a command whose whole output is one number.
func countSingleNumber(ctx context.Context, client docker.Client, containerID, password string, cmd []string) (int, error) {
	var out strings.Builder
	result, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{Cmd: cmd, Stdout: &out})
	if err != nil {
		return 0, fmt.Errorf("count restored objects: %w", err)
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("counting restored objects failed: %s",
			redactSecret(strings.TrimSpace(result.Stderr), password))
	}
	text := strings.TrimSpace(out.String())
	count, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("could not read the object count from %q", text)
	}
	return count, nil
}

func waitReady(ctx context.Context, client docker.Client, containerID string, plan restorePlan, password string) error {
	deadline := time.Now().Add(readyTimeout)
	var lastDetail string

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}

		result, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{
			Cmd:    plan.ready("", password),
			Stdout: discard{},
		})
		if err == nil && result.ExitCode == 0 {
			// Database images start a temporary server while initialising and
			// then restart it. Answering once is not the same as being ready,
			// so readiness has to hold rather than merely occur.
			time.Sleep(2 * time.Second)
			again, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{
				Cmd:    plan.ready("", password),
				Stdout: discard{},
			})
			if err == nil && again.ExitCode == 0 {
				return nil
			}
		}
		if err != nil {
			lastDetail = err.Error()
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("the verification server did not become ready within %s (%s)",
		readyTimeout, lastDetail)
}

// countPostgresTables counts user tables across every restored database.
//
// Two steps because PostgreSQL's catalogue is per-database: one connection can
// only see its own. A dump that recreates "kunden" leaves the connection's
// default database empty, and counting there would report a healthy restore as
// empty.
func countPostgresTables(ctx context.Context, client docker.Client, containerID, password string) (int, error) {
	// The throwaway verification server always runs as the image's own
	// superuser; a live one may not.
	return countPostgresTablesAs(ctx, client, containerID, "postgres", password)
}

// countPostgresTablesAs is the same count against a server whose superuser is
// whatever the project configured.
func countPostgresTablesAs(ctx context.Context, client docker.Client, containerID, user, password string) (int, error) {
	var list strings.Builder
	result, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{
		Cmd: []string{"psql", "-U", user, "-d", "postgres", "-t", "-A", "-c",
			"select datname from pg_database where datistemplate = false"},
		Stdout: &list,
	})
	if err != nil {
		return 0, fmt.Errorf("list restored databases: %w", err)
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("listing restored databases failed: %s",
			redactSecret(strings.TrimSpace(result.Stderr), password))
	}

	total := 0
	for _, line := range strings.Split(list.String(), "\n") {
		database := strings.TrimSpace(line)
		if database == "" {
			continue
		}

		var count strings.Builder
		result, err := client.ExecInContainer(ctx, containerID, docker.ExecRequest{
			Cmd: []string{"psql", "-U", user, "-d", database, "-t", "-A", "-c",
				"select count(*) from pg_tables where schemaname not in ('pg_catalog','information_schema')"},
			Stdout: &count,
		})
		if err != nil || result.ExitCode != 0 {
			// One unreadable database should not sink the whole count; the
			// others still answer the question.
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(count.String())); err == nil {
			total += n
		}
	}
	return total, nil
}

// throwawayPassword generates credentials for a server that lives for seconds.
func throwawayPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("dbdump: generate verification credentials: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// discard swallows output the check does not need. The dump's own text is not
// worth keeping: it is the exit code and the object count that answer the
// question.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
