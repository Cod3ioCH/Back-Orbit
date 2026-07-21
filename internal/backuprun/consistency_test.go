package backuprun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/projectanalyzer"
)

type fakeBlueprints struct {
	blueprint projectanalyzer.Blueprint
	err       error
}

func (f fakeBlueprints) Get(context.Context, string) (projectanalyzer.Blueprint, error) {
	return f.blueprint, f.err
}

func withFindings(findings ...projectanalyzer.Finding) fakeBlueprints {
	return fakeBlueprints{blueprint: projectanalyzer.Blueprint{Findings: findings}}
}

func database(technology, service string) projectanalyzer.Finding {
	return projectanalyzer.Finding{Kind: "database", Technology: technology, Service: service}
}

// TestServerDatabasesAreReportedAsFileCopies is the gap this closes. The
// analyzer recommends a dump; nothing in Back-Orbit performs one yet. Left
// unsaid, the project page shows a green protection summary listing PostgreSQL
// and the run reports "Verified" — over a file-level copy of a live cluster.
func TestServerDatabasesAreReportedAsFileCopies(t *testing.T) {
	for _, technology := range []string{"postgresql", "mysql", "mariadb", "mongodb", "redis", "valkey"} {
		t.Run(technology, func(t *testing.T) {
			warnings := databaseConsistencyWarnings(
				context.Background(), withFindings(database(technology, "db")), "project-1")

			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want exactly one: %v", len(warnings), warnings)
			}
			// The warning has to carry the consequence, not just the fact.
			for _, needed := range []string{"db", "may not start when restored"} {
				if !strings.Contains(warnings[0], needed) {
					t.Errorf("warning does not mention %q: %s", needed, warnings[0])
				}
			}
		})
	}
}

// TestSQLiteIsNotWarnedAbout: staging already re-takes SQLite through SQLite's
// own online backup, so a snapshot holds a consistent copy. Warning about it
// would teach people to ignore the warning that matters.
func TestSQLiteIsNotWarnedAbout(t *testing.T) {
	warnings := databaseConsistencyWarnings(
		context.Background(), withFindings(database("sqlite", "app")), "project-1")

	if len(warnings) != 0 {
		t.Fatalf("SQLite is captured consistently and must not be warned about: %v", warnings)
	}
}

// TestOneWarningPerTechnology keeps a project with several PostgreSQL services
// from producing the same sentence three times.
func TestOneWarningPerTechnology(t *testing.T) {
	warnings := databaseConsistencyWarnings(context.Background(), withFindings(
		database("postgresql", "primary"),
		database("postgresql", "replica"),
		database("postgresql", "primary"),
	), "project-1")

	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want one: %v", len(warnings), warnings)
	}
	for _, service := range []string{"primary", "replica"} {
		if !strings.Contains(warnings[0], service) {
			t.Errorf("the warning should name service %q: %s", service, warnings[0])
		}
	}
}

// TestNonDatabaseFindingsAreIgnored: storage and secret findings say nothing
// about consistency.
func TestNonDatabaseFindingsAreIgnored(t *testing.T) {
	warnings := databaseConsistencyWarnings(context.Background(), withFindings(
		projectanalyzer.Finding{Kind: "storage", Technology: "volume"},
		projectanalyzer.Finding{Kind: "secret", Technology: "compose-secret"},
	), "project-1")

	if len(warnings) != 0 {
		t.Fatalf("got %v, want nothing", warnings)
	}
}

// TestMissingAnalysisDoesNotBreakABackup: a project that was never analyzed
// still gets backed up. The check is an addition to a backup, not a gate on it.
func TestMissingAnalysisDoesNotBreakABackup(t *testing.T) {
	cases := map[string]BlueprintSource{
		"no analyzer wired": nil,
		"never analyzed":    fakeBlueprints{err: errors.New("not found")},
		"nothing found":     withFindings(),
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if warnings := databaseConsistencyWarnings(context.Background(), source, "project-1"); len(warnings) != 0 {
				t.Fatalf("got %v, want nothing", warnings)
			}
		})
	}
}
