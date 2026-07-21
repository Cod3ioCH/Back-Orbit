package projectanalyzer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
)

func TestDatabaseDetectorUsesEvidenceAndConfidence(t *testing.T) {
	input := Input{Services: []ServiceEvidence{{Name: "db", Image: "postgres:17", EnvironmentNames: []string{"POSTGRES_PASSWORD", "POSTGRES_DB"}}}}
	findings := databaseDetector{}.Detect(input)
	if len(findings) != 1 {
		t.Fatalf("expected one database finding, got %#v", findings)
	}
	if findings[0].Technology != "postgresql" || findings[0].Confidence != ConfidenceConfirmed {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestAnalyzeNeverPersistsEnvironmentValues(t *testing.T) {
	db := dbtest.Open(t)
	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	projectService := projects.NewService(db, nil, recorder)
	dir := t.TempDir()
	compose := `services:
  db:
    image: postgres:17
    environment:
      POSTGRES_USER: orbit
      POSTGRES_PASSWORD: super-secret-value
    volumes:
      - pgdata:/var/lib/postgresql/data
secrets:
  database_password:
    file: ./password.txt
volumes:
  pgdata: {}
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := projectService.Register(context.Background(), "", "test-project", dir)
	if err != nil {
		t.Fatal(err)
	}
	bp, err := NewService(db, projectService, nil, recorder).Analyze(context.Background(), record.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(bp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-value") || strings.Contains(string(raw), "orbit") {
		t.Fatalf("blueprint leaked an environment value: %s", raw)
	}
	if !strings.Contains(string(raw), "POSTGRES_USER") {
		t.Fatalf("expected environment key evidence, got %s", raw)
	}
}

func TestFingerprintIsStable(t *testing.T) {
	findings := []Finding{{ID: "database:db:postgresql", Kind: "database", Technology: "postgresql", Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: "compose", Subject: "db", Detail: "image matches postgresql"}}}}
	a, err := fingerprint(findings)
	if err != nil {
		t.Fatal(err)
	}
	b, err := fingerprint(findings)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("fingerprint changed: %s != %s", a, b)
	}
}

func TestAnalyzeReportsDriftAfterConfirmedProjectChanges(t *testing.T) {
	db := dbtest.Open(t)
	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	projectService := projects.NewService(db, nil, recorder)
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  app:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := projectService.Register(context.Background(), "", "drift-test", dir)
	if err != nil {
		t.Fatal(err)
	}
	analyzer := NewService(db, projectService, nil, recorder)
	if _, err := analyzer.Analyze(context.Background(), record.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzer.Confirm(context.Background(), record.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, []byte("services:\n  app:\n    image: nginx:latest\n  cache:\n    image: redis:7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bp, err := analyzer.Analyze(context.Background(), record.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bp.Drifted {
		t.Fatal("expected a changed confirmed project to report drift")
	}
}

func TestFindSQLiteInNamedVolumeUsesReadOnlyHelperAndCleansUp(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	contents := append([]byte("SQLite format 3\x00"), []byte("test-data")...)
	if err := tw.WriteHeader(&tar.Header{Name: "back-orbit.db", Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	fake := docker.NewFakeClient()
	fake.FakeSelfImage = "back-orbit:test"
	fake.ArchiveTar = archive.Bytes()
	found, warnings := findSQLiteInSources(context.Background(), fake, []projects.BackupSource{{Kind: projects.SourceVolume, Name: "project-data"}})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if len(found) != 1 || found[0] != "volume:project-data/back-orbit.db" {
		t.Fatalf("unexpected SQLite findings: %#v", found)
	}
	if len(fake.CreatedContainers) != 1 || fake.CreatedContainers[0].Writable {
		t.Fatalf("expected one read-only helper, got %#v", fake.CreatedContainers)
	}
	if len(fake.RemovedContainers) != 1 {
		t.Fatalf("expected helper cleanup, got %#v", fake.RemovedContainers)
	}
}

func TestFindSQLiteInBindMount(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	contents := append([]byte("SQLite format 3\x00"), []byte("test-data")...)
	if err := tw.WriteHeader(&tar.Header{Name: "buergergemeinde.db", Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	fake := docker.NewFakeClient()
	fake.FakeSelfImage = "back-orbit:test"
	fake.ArchiveTar = archive.Bytes()
	found, warnings := findSQLiteInSources(context.Background(), fake, []projects.BackupSource{{Kind: projects.SourceBind, Name: "/srv/project/data"}})
	if len(warnings) != 0 || len(found) != 1 || found[0] != "bind:/srv/project/data/buergergemeinde.db" {
		t.Fatalf("unexpected result: found=%#v warnings=%#v", found, warnings)
	}
}

func TestClassifySQLitePathsExcludesBackupCopies(t *testing.T) {
	active, artifacts := classifySQLitePaths([]string{
		"bind:/srv/project/data/buergergemeinde.db",
		"bind:/srv/project/data/backups/backup-20260718.db",
	})
	if len(active) != 1 || !strings.HasSuffix(active[0], "buergergemeinde.db") {
		t.Fatalf("unexpected active databases: %#v", active)
	}
	if len(artifacts) != 1 || !strings.Contains(artifacts[0], "/backups/") {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
}
