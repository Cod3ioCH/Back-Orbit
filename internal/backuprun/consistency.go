package backuprun

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/Cod3ioCH/Back-Orbit/internal/projectanalyzer"
)

// BlueprintSource reports what a project's analysis found.
//
// A narrow interface rather than the analyzer service itself: the runner needs
// one answer from it, and a fake in a test should not have to be a database.
type BlueprintSource interface {
	Get(ctx context.Context, projectID string) (projectanalyzer.Blueprint, error)
}

// serverDatabases are the engines whose files must not simply be copied while
// they are running.
//
// SQLite is deliberately absent: staging already re-takes it through SQLite's
// own online backup, so a snapshot holds a consistent copy. The engines here
// have no such treatment yet — their data directories are captured as plain
// files, which for a running server is a copy of a moving target.
var serverDatabases = map[string]string{
	"postgresql": "PostgreSQL",
	"mysql":      "MySQL",
	"mariadb":    "MariaDB",
	"mongodb":    "MongoDB",
	"redis":      "Redis",
	"valkey":     "Valkey",
}

// databaseConsistencyWarnings reports the databases this backup captured as
// plain files rather than as a dump.
//
// This closes the gap between what the analyzer promises and what a backup
// currently does. The analyzer tells the operator "create a logical dump with
// pg_dump before snapshotting persistent storage" — sound advice that nothing
// in Back-Orbit yet carries out. Left unsaid, the combination is worse than no
// analysis at all: the project page shows a green protection summary listing
// PostgreSQL, the run reports "Verified", and what is actually in the snapshot
// is a file-level copy of a live cluster. Verification confirms the snapshot is
// readable; it says nothing about whether the database inside it will start.
//
// A warning rather than a refusal, because refusing would leave the operator
// with no backup at all — and for a stopped service the file copy is perfectly
// good. What must not happen is that the limitation goes unmentioned.
func databaseConsistencyWarnings(ctx context.Context, source BlueprintSource, projectID string) []string {
	if source == nil || projectID == "" {
		return nil
	}

	blueprint, err := source.Get(ctx, projectID)
	if err != nil {
		// No analysis, or it could not be read. Not worth failing a backup
		// over, but the operator should know the check did not happen.
		slog.Debug("backuprun: no protection blueprint available for consistency warnings",
			"project", projectID, "error", err)
		return nil
	}

	// Deduplicated by technology: three PostgreSQL services produce one
	// sentence about PostgreSQL, not three.
	services := map[string][]string{}
	for _, finding := range blueprint.Findings {
		if finding.Kind != "database" {
			continue
		}
		label, relevant := serverDatabases[finding.Technology]
		if !relevant {
			continue
		}
		if finding.Service != "" {
			services[label] = appendUnique(services[label], finding.Service)
		} else if _, seen := services[label]; !seen {
			services[label] = nil
		}
	}
	if len(services) == 0 {
		return nil
	}

	labels := make([]string, 0, len(services))
	for label := range services {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	warnings := make([]string, 0, len(labels))
	for _, label := range labels {
		where := ""
		if names := services[label]; len(names) > 0 {
			sort.Strings(names)
			where = fmt.Sprintf(" (service %s)", strings.Join(names, ", "))
		}
		warnings = append(warnings, fmt.Sprintf(
			"%s was detected in this project%s, and its files were copied as they were rather than "+
				"exported with a dump. If the database was running during this backup, the copy may be "+
				"inconsistent and may not start when restored. Until Back-Orbit performs the dump itself, "+
				"stop the service before backing up, or take a dump alongside this snapshot.",
			label, where))
	}
	return warnings
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
