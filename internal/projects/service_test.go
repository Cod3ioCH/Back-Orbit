package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
)

func newTestService(t *testing.T, dockerClient docker.Client) *Service {
	t.Helper()
	db := dbtest.Open(t)
	recorder := events.NewRecorder(events.NewStore(db), events.NewBroker())
	return NewService(db, dockerClient, recorder)
}

func TestRegisterAndList(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}")

	record, err := svc.Register(ctx, "actor-1", "myproject", dir)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if record.Source != SourceRegistered {
		t.Fatalf("expected source %q, got %q", SourceRegistered, record.Source)
	}
	if len(record.ComposeFiles) != 1 || record.ComposeFiles[0] != "compose.yml" {
		t.Fatalf("expected to detect compose.yml, got %+v", record.ComposeFiles)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != record.ID {
		t.Fatalf("expected the registered project in the list, got %+v", list)
	}
}

func TestRegisterRejectsRelativePath(t *testing.T) {
	svc := newTestService(t, nil)

	_, err := svc.Register(context.Background(), "actor-1", "myproject", "relative/path")
	if err != ErrInvalidPath {
		t.Fatalf("expected ErrInvalidPath, got %v", err)
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	svc := newTestService(t, nil)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := svc.Register(ctx, "actor-1", "myproject", dir); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := svc.Register(ctx, "actor-1", "myproject", dir)
	if err != ErrProjectExists {
		t.Fatalf("expected ErrProjectExists, got %v", err)
	}
}

func TestRegisterSucceedsForUnreadablePathWithNoComposeFiles(t *testing.T) {
	svc := newTestService(t, nil)

	record, err := svc.Register(context.Background(), "actor-1", "myproject", "/this/path/does/not/exist")
	if err != nil {
		t.Fatalf("expected registration to succeed even for an unreadable path, got: %v", err)
	}
	if len(record.ComposeFiles) != 0 {
		t.Fatalf("expected no compose files detected, got %+v", record.ComposeFiles)
	}
}

func TestScanUpsertsDiscoveredProjects(t *testing.T) {
	fake := docker.NewFakeClient(docker.ComposeProject{
		Name:        "discovered-app",
		WorkingDir:  "/srv/discovered-app",
		ConfigFiles: []string{"/srv/discovered-app/compose.yml"},
	})
	svc := newTestService(t, fake)
	ctx := context.Background()

	records, err := svc.Scan(ctx, "actor-1")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(records) != 1 || records[0].Name != "discovered-app" {
		t.Fatalf("expected discovered-app to be registered, got %+v", records)
	}
	if records[0].Source != SourceDiscovered {
		t.Fatalf("expected source %q, got %q", SourceDiscovered, records[0].Source)
	}

	// Scanning again must not create a duplicate row.
	records, err = svc.Scan(ctx, "actor-1")
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected scan to be idempotent, got %d records", len(records))
	}
}

func TestGetAttachesLiveDockerState(t *testing.T) {
	fake := docker.NewFakeClient(docker.ComposeProject{
		Name:       "discovered-app",
		WorkingDir: "/srv/discovered-app",
		Containers: []docker.Container{{ID: "c1", Name: "discovered-app-web-1"}},
	})
	svc := newTestService(t, fake)
	ctx := context.Background()

	records, err := svc.Scan(ctx, "actor-1")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	detail, err := svc.Get(ctx, records[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !detail.DockerAvailable {
		t.Fatalf("expected DockerAvailable, got warning: %s", detail.DockerWarning)
	}
	if len(detail.Containers) != 1 {
		t.Fatalf("expected 1 live container, got %d", len(detail.Containers))
	}
}

func TestGetSurfacesDockerUnreachableAsWarningNotError(t *testing.T) {
	fake := docker.NewFakeClient()
	fake.StatusResult = docker.Status{Connected: false, Error: "connection refused"}
	svc := newTestService(t, fake)
	ctx := context.Background()

	record, err := svc.Register(ctx, "actor-1", "myproject", t.TempDir())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	detail, err := svc.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("expected Get to succeed even when Docker is unreachable, got error: %v", err)
	}
	if detail.DockerAvailable {
		t.Fatal("expected DockerAvailable to be false")
	}
	if detail.DockerWarning == "" {
		t.Fatal("expected a DockerWarning explaining why live state is unavailable")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
