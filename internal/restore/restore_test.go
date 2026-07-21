package restore

import (
	"context"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/backuprun"
)

type fakeSnapshots struct{ snapshot *backuprun.Snapshot }

func (f fakeSnapshots) GetSnapshot(context.Context, string) (*backuprun.Snapshot, error) {
	return f.snapshot, nil
}

func testRunner() *Runner {
	return &Runner{snapshots: fakeSnapshots{&backuprun.Snapshot{ID: "snapshot-1", FilesCount: 3, SizeBytes: 42, Manifest: backuprun.Manifest{Project: "notes", Volumes: []backuprun.VolumeManifest{{Name: "notes_data", Kind: "volume", PathInSnapshot: "/var/lib/back-orbit/staging/notes/notes_data", Files: 3, Bytes: 42}}}}}}
}

func TestPreviewExtractIsNonDestructive(t *testing.T) {
	p, err := testRunner().Preview(context.Background(), PreviewRequest{SnapshotID: "snapshot-1", Mode: ModeExtract})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Supported || p.Destructive {
		t.Fatalf("unexpected preview: %+v", p)
	}
	if len(p.Items) != 1 || p.Items[0].Name != "notes_data" {
		t.Fatalf("unexpected items: %+v", p.Items)
	}
}

func TestPreviewBlocksLegacyInPlaceAndClone(t *testing.T) {
	for _, mode := range []Mode{ModeInPlace, ModeClone} {
		p, err := testRunner().Preview(context.Background(), PreviewRequest{SnapshotID: "snapshot-1", Mode: mode, NewProjectName: "notes-copy"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Supported || len(p.Blockers) == 0 {
			t.Fatalf("mode %s was not blocked: %+v", mode, p)
		}
	}
}
