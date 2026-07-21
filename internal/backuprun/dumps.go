package backuprun

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbdump"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/projectanalyzer"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
)

// dumpEngines maps a detected technology to the exporter that can handle it.
//
// Only PostgreSQL so far. The others stay on the file-copy path and keep their
// warning, which is the honest arrangement: an engine is listed here once it
// can actually be exported, never before.
var dumpEngines = map[string]bool{"postgresql": true}

// dumpResult pairs a written dump with the service it came from.
type dumpResult struct {
	dbdump.Result
	key string
}

// dumpDatabases exports every detected database Back-Orbit can export, into
// the staged tree so the dump travels inside the same snapshot as the files.
//
// One snapshot holding both is deliberate. A dump stored apart from the volume
// it was taken from is a second thing to find, keep and match up at the worst
// possible moment; together they restore as one unit.
//
// A failed dump never fails the backup. The file copy underneath is still
// worth having, and the run keeps its warning saying the export did not
// happen — losing the whole snapshot because one database refused to talk
// would be the wrong trade.
func (r *Runner) dumpDatabases(
	ctx context.Context,
	run *Run,
	project projects.Detail,
	stagingDir string,
) []dumpResult {
	if r.blueprints == nil || r.docker == nil || run.ProjectID == "" {
		return nil
	}

	blueprint, err := r.blueprints.Get(ctx, run.ProjectID)
	if err != nil {
		return nil
	}

	containers := map[string]docker.Container{}
	for _, container := range project.Containers {
		if container.Service != "" {
			containers[container.Service] = container
		}
	}

	var results []dumpResult
	for _, finding := range blueprint.Findings {
		if finding.Kind != "database" || !dumpEngines[finding.Technology] {
			continue
		}
		// A "possible" finding is one where only the image name suggested a
		// database — an exporter or a migration job lands here. Running a dump
		// against it would produce a confusing failure at best.
		if finding.Confidence == projectanalyzer.ConfidencePossible {
			continue
		}

		container, running := containers[finding.Service]
		if !running || container.ID == "" {
			run.Warnings = append(run.Warnings, fmt.Sprintf(
				"%s in service %s could not be exported because its container is not running; "+
					"its files were copied as they are.", finding.Technology, finding.Service))
			continue
		}

		result, err := r.dumpOne(ctx, finding, container, stagingDir)
		if err != nil {
			run.Warnings = append(run.Warnings, fmt.Sprintf(
				"%s in service %s could not be exported (%v); its files were copied as they are, "+
					"which may be inconsistent if the database was running.",
				finding.Technology, finding.Service, err))
			continue
		}

		results = append(results, dumpResult{Result: result, key: finding.Service + ":" + finding.Technology})
		slog.Info("backuprun: database exported",
			"run", run.ID, "service", finding.Service, "technology", finding.Technology, "bytes", result.Bytes)
	}
	return results
}

func (r *Runner) dumpOne(
	ctx context.Context,
	finding projectanalyzer.Finding,
	container docker.Container,
	stagingDir string,
) (dbdump.Result, error) {
	// Only the user name, never the password: the dump runs inside the
	// container over its local socket, where the server trusts its own
	// operating-system user.
	user, err := r.docker.ContainerEnvValue(ctx, container.ID, "POSTGRES_USER")
	if err != nil {
		return dbdump.Result{}, fmt.Errorf("read the database user: %w", err)
	}

	return dbdump.PostgreSQL(ctx, r.docker, dbdump.Target{
		Technology:  finding.Technology,
		Service:     finding.Service,
		ContainerID: container.ID,
		User:        user,
	}, stagingDir)
}

// dumpedKeys reports which databases were exported, so the consistency check
// only warns about the ones that were not.
func dumpedKeys(results []dumpResult) map[string]bool {
	keys := map[string]bool{}
	for _, result := range results {
		keys[result.key] = true
	}
	return keys
}
