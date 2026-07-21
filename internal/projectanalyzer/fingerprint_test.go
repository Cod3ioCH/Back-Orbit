package projectanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
)

// TestTheFingerprintIsStableWhenNothingChanged.
//
// The fingerprint is the whole drift mechanism: a change to it tells the
// operator their project moved and the blueprint needs another look. Services
// are read out of a Compose service map, whose iteration order Go randomises
// on purpose, so an unsorted list produced a different fingerprint on every
// analysis — a project nobody had touched kept announcing that it had drifted.
// A warning that fires at random is one people learn to click past.
func TestTheFingerprintIsStableWhenNothingChanged(t *testing.T) {
	service, projectID := analyzerWithProject(t, `services:
  app:
    image: nextcloud:31-apache
  cache:
    image: redis:7-alpine
  db:
    image: postgres:17-alpine
  proxy:
    image: nginx:1
  worker:
    image: busybox:1
`)

	first, err := service.Analyze(context.Background(), projectID, "")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 12; run++ {
		again, err := service.Analyze(context.Background(), projectID, "")
		if err != nil {
			t.Fatal(err)
		}
		if again.Fingerprint != first.Fingerprint {
			t.Fatalf("run %d fingerprinted the same project differently: %s vs %s",
				run, again.Fingerprint[:12], first.Fingerprint[:12])
		}
		if again.Drifted {
			t.Fatalf("run %d reported drift on an unchanged project", run)
		}
	}
}

// TestTemplateMatchesAreStableAcrossRuns, for the same reason: which image
// fills which role must not depend on the order the services were read in.
func TestTemplateMatchesAreStableAcrossRuns(t *testing.T) {
	service, projectID := analyzerWithProject(t, `services:
  admin:
    image: mongo-express:1.0.2
  mongo:
    image: mongo:7
    volumes:
      - mongo-data:/data/db
volumes:
  mongo-data: {}
`)

	var first []string
	for run := 0; run < 12; run++ {
		blueprint, err := service.Analyze(context.Background(), projectID, "")
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, match := range blueprint.TemplateMatches {
			ids = append(ids, match.TemplateID)
		}
		if run == 0 {
			first = ids
			continue
		}
		if len(ids) != len(first) {
			t.Fatalf("run %d matched %v, first run matched %v", run, ids, first)
		}
		for i := range ids {
			if ids[i] != first[i] {
				t.Fatalf("run %d matched %v, first run matched %v", run, ids, first)
			}
		}
	}
	if len(first) == 0 {
		t.Fatal("the MongoDB topology matched no template at all")
	}
}

func analyzerWithProject(t *testing.T, compose string) (*Service, string) {
	t.Helper()
	db := dbtest.Open(t)
	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	projectService := projects.NewService(db, nil, recorder)

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := projectService.Register(context.Background(), "", "fingerprint-lab", directory)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, projectService, nil, recorder), project.ID
}
