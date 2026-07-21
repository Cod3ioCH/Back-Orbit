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

func TestAnalyzeMatchesApplicationProtectionTemplate(t *testing.T) {
	db := dbtest.Open(t)
	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	projectService := projects.NewService(db, nil, recorder)
	directory := t.TempDir()
	compose := `services:
  wordpress:
    image: wordpress:6-apache
    volumes:
      - wordpress-data:/var/www/html
  database:
    image: mariadb:11
    environment:
      MARIADB_DATABASE: wordpress
      MARIADB_PASSWORD: value-must-never-enter-analysis
    volumes:
      - database-data:/var/lib/mysql
volumes:
  wordpress-data: {}
  database-data: {}
`
	if err := os.WriteFile(filepath.Join(directory, "compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := projectService.Register(context.Background(), "", "wordpress-lab", directory)
	if err != nil {
		t.Fatal(err)
	}
	blueprint, err := NewService(db, projectService, nil, recorder).Analyze(context.Background(), project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(blueprint.TemplateMatches) == 0 || blueprint.TemplateMatches[0].TemplateID != "wordpress-mariadb" {
		t.Fatalf("expected WordPress template match, got %#v", blueprint.TemplateMatches)
	}
	match := blueprint.TemplateMatches[0]
	if match.Score != 100 || match.Version != "1.0.0" {
		t.Fatalf("unexpected match quality: %#v", match)
	}
	if len(match.Plan.DatabaseStrategy) != 1 || match.Plan.DatabaseStrategy[0] != "mariadb-dump" {
		t.Fatalf("unexpected generated database strategy: %#v", match.Plan)
	}
}
