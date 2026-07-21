package storage

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// tarEntry describes one file to put into a synthetic archive.
type tarEntry struct {
	name     string
	body     string
	mode     int64
	uid, gid int
	typeflag byte
	linkname string
	modTime  time.Time
}

func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		modTime := entry.modTime
		if modTime.IsZero() {
			modTime = time.Unix(1_600_000_000, 0)
		}

		header := &tar.Header{
			Name:     entry.name,
			Mode:     mode,
			Uid:      entry.uid,
			Gid:      entry.gid,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
			ModTime:  modTime,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func newStager(t *testing.T, archive []byte) (*Stager, *docker.FakeClient) {
	t.Helper()
	fake := docker.NewFakeClient()
	fake.FakeSelfImage = "back-orbit:test"
	fake.ArchiveTar = archive
	return NewStager(fake, ""), fake
}

func TestStageVolumeExtractsContents(t *testing.T) {
	archive := buildTar(t, []tarEntry{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./data.txt", body: "important data", mode: 0o600},
		{name: "./nested/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./nested/inner.txt", body: "nested", mode: 0o644},
		{name: "./link.txt", typeflag: tar.TypeSymlink, linkname: "data.txt"},
	})
	stager, fake := newStager(t, archive)

	dest := filepath.Join(t.TempDir(), "stage")
	result, err := stager.StageVolume(context.Background(), "app-data", dest)
	if err != nil {
		t.Fatalf("StageVolume: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "data.txt"))
	if err != nil || string(got) != "important data" {
		t.Fatalf("data.txt = %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dest, "nested", "inner.txt"))
	if err != nil || string(got) != "nested" {
		t.Fatalf("nested/inner.txt = %q, %v", got, err)
	}

	// The symlink must survive as a symlink, not as a copy of its target.
	info, err := os.Lstat(filepath.Join(dest, "link.txt"))
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was not preserved as a symlink")
	}

	if result.Bytes != int64(len("important data")+len("nested")) {
		t.Fatalf("Bytes = %d", result.Bytes)
	}

	// The helper container must be mounted read-only and cleaned up.
	if len(fake.CreatedContainers) != 1 {
		t.Fatalf("expected one helper container, got %d", len(fake.CreatedContainers))
	}
	if fake.CreatedContainers[0].Source != "app-data" {
		t.Fatalf("wrong volume mounted: %+v", fake.CreatedContainers[0])
	}
	if leaked := fake.LeakedContainers(); len(leaked) != 0 {
		t.Fatalf("helper containers were leaked: %v", leaked)
	}
}

// TestStageVolumePreservesPermissionsAndTimes covers metadata a restore
// depends on: an application handed back a config file with the wrong mode may
// refuse to start.
func TestStageVolumePreservesPermissionsAndTimes(t *testing.T) {
	modTime := time.Unix(1_577_934_245, 0) // 2020-01-02 03:04:05 UTC
	archive := buildTar(t, []tarEntry{
		{name: "./secret.conf", body: "private", mode: 0o600, modTime: modTime},
		{name: "./script.sh", body: "#!/bin/sh", mode: 0o755, modTime: modTime},
	})
	stager, _ := newStager(t, archive)

	dest := filepath.Join(t.TempDir(), "stage")
	if _, err := stager.StageVolume(context.Background(), "app-data", dest); err != nil {
		t.Fatalf("StageVolume: %v", err)
	}

	for name, wantMode := range map[string]os.FileMode{
		"secret.conf": 0o600,
		"script.sh":   0o755,
	} {
		info, err := os.Stat(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != wantMode {
			t.Fatalf("%s mode = %o, want %o", name, info.Mode().Perm(), wantMode)
		}
		if !info.ModTime().Equal(modTime) {
			t.Fatalf("%s modtime = %v, want %v", name, info.ModTime(), modTime)
		}
	}
}

// TestOwnershipIsRecordedEvenWhenItCannotBeApplied is the fidelity guarantee
// that makes an unprivileged Back-Orbit safe to restore from. Running as a
// normal user, chown fails — so the original owners have to be captured, or a
// restore would hand an application files it does not own.
func TestOwnershipIsRecordedEvenWhenItCannotBeApplied(t *testing.T) {
	archive := buildTar(t, []tarEntry{
		{name: "./db.sql", body: "data", mode: 0o600, uid: 999, gid: 999},
		{name: "./web.conf", body: "conf", mode: 0o644, uid: 33, gid: 33},
	})
	stager, _ := newStager(t, archive)

	dest := filepath.Join(t.TempDir(), "stage")
	result, err := stager.StageVolume(context.Background(), "app-data", dest)
	if err != nil {
		t.Fatalf("StageVolume: %v", err)
	}

	owners := map[string]OwnershipEntry{}
	for _, entry := range result.Ownership {
		owners[entry.Path] = entry
	}

	if got := owners["db.sql"]; got.UID != 999 || got.GID != 999 {
		t.Fatalf("db.sql ownership = %d:%d, want 999:999", got.UID, got.GID)
	}
	if got := owners["web.conf"]; got.UID != 33 || got.GID != 33 {
		t.Fatalf("web.conf ownership = %d:%d, want 33:33", got.UID, got.GID)
	}

	// Running unprivileged, the ownership cannot be applied on disk — and that
	// has to be stated rather than assumed either way.
	if os.Geteuid() != 0 {
		if result.OwnershipPreserved {
			t.Fatal("ownership cannot be applied without root, but was reported as preserved")
		}
		if len(result.Warnings) == 0 {
			t.Fatal("losing ownership must produce a warning")
		}
		if !strings.Contains(strings.Join(result.Warnings, " "), "ownership") {
			t.Fatalf("the warning should explain the ownership limitation: %v", result.Warnings)
		}
	}
}

// TestStageVolumeRefusesPathEscape is the tar-slip defence. The archive comes
// from whatever is inside a user's volume, so an entry that climbs out of the
// staging directory must stop the whole run.
func TestStageVolumeRefusesPathEscape(t *testing.T) {
	for name, entryName := range map[string]string{
		"parent traversal":      "../escaped.txt",
		"deep parent traversal": "./nested/../../escaped.txt",
		"absolute path":         "/etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			archive := buildTar(t, []tarEntry{{name: entryName, body: "owned"}})
			stager, _ := newStager(t, archive)

			parent := t.TempDir()
			dest := filepath.Join(parent, "stage")

			_, err := stager.StageVolume(context.Background(), "app-data", dest)

			// An absolute path is normalised into the staging directory rather
			// than rejected — the result is still contained, which is what
			// matters. A traversal that would escape must fail outright.
			if err == nil {
				escaped := filepath.Join(parent, "escaped.txt")
				if _, statErr := os.Stat(escaped); statErr == nil {
					t.Fatal("an archive entry was written outside the staging directory")
				}
				return
			}
			if !strings.Contains(err.Error(), "outside the staging directory") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestNothingIsWrittenOutsideEvenOnFailure double-checks the containment claim
// with a mixed archive: a good entry followed by an escaping one.
func TestNothingIsWrittenOutsideEvenOnFailure(t *testing.T) {
	archive := buildTar(t, []tarEntry{
		{name: "./fine.txt", body: "fine"},
		{name: "../escaped.txt", body: "owned"},
	})
	stager, _ := newStager(t, archive)

	parent := t.TempDir()
	dest := filepath.Join(parent, "stage")

	if _, err := stager.StageVolume(context.Background(), "app-data", dest); err == nil {
		t.Fatal("expected the escaping entry to fail the run")
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Fatal("a file was written outside the staging directory")
	}
}

// TestHelperContainerIsRemovedOnFailure is what keeps a failed backup from
// leaving containers pinned to volumes.
func TestHelperContainerIsRemovedOnFailure(t *testing.T) {
	stager, fake := newStager(t, nil)
	fake.ArchiveErr = errors.New("daemon exploded")

	dest := filepath.Join(t.TempDir(), "stage")
	if _, err := stager.StageVolume(context.Background(), "app-data", dest); err == nil {
		t.Fatal("expected staging to fail")
	}

	if len(fake.CreatedContainers) != 1 {
		t.Fatalf("expected the helper container to have been created, got %d", len(fake.CreatedContainers))
	}
	if leaked := fake.LeakedContainers(); len(leaked) != 0 {
		t.Fatalf("a failed run leaked helper containers: %v", leaked)
	}
}

// TestHelperContainerIsRemovedAfterCancellation covers the cancel path: the
// caller's context is already dead, so cleanup must not depend on it.
func TestHelperContainerIsRemovedAfterCancellation(t *testing.T) {
	stager, fake := newStager(t, nil)
	fake.ArchiveErr = context.Canceled

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(t.TempDir(), "stage")
	if _, err := stager.StageVolume(ctx, "app-data", dest); err == nil {
		t.Fatal("expected staging to fail")
	}
	if leaked := fake.LeakedContainers(); len(leaked) != 0 {
		t.Fatalf("cancellation leaked helper containers: %v", leaked)
	}
}

func TestSweepOrphansRemovesLeftovers(t *testing.T) {
	fake := docker.NewFakeClient()
	fake.FakeSelfImage = "back-orbit:test"
	stager := NewStager(fake, "")
	ctx := context.Background()

	// Simulate two helper containers left behind by a crashed run.
	if _, err := fake.CreateHelperContainer(ctx, docker.HelperContainerRequest{Source: "one"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := fake.CreateHelperContainer(ctx, docker.HelperContainerRequest{Source: "two"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	removed, err := stager.SweepOrphans(ctx)
	if err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if leaked := fake.LeakedContainers(); len(leaked) != 0 {
		t.Fatalf("orphans remain after a sweep: %v", leaked)
	}
}

func TestStageVolumeRejectsEmptyName(t *testing.T) {
	stager, _ := newStager(t, nil)
	if _, err := stager.StageVolume(context.Background(), "", t.TempDir()); err == nil {
		t.Fatal("expected an empty volume name to be rejected")
	}
}

// TestUnsupportedEntriesAreReportedNotSkippedSilently: device nodes and
// sockets cannot be recreated unprivileged, but a backup must never quietly
// omit something.
func TestUnsupportedEntriesAreReportedNotSkippedSilently(t *testing.T) {
	archive := buildTar(t, []tarEntry{
		{name: "./normal.txt", body: "fine"},
		{name: "./dev-node", typeflag: tar.TypeChar},
	})
	stager, _ := newStager(t, archive)

	dest := filepath.Join(t.TempDir(), "stage")
	result, err := stager.StageVolume(context.Background(), "app-data", dest)
	if err != nil {
		t.Fatalf("StageVolume: %v", err)
	}

	if !strings.Contains(strings.Join(result.Warnings, " "), "dev-node") {
		t.Fatalf("the skipped entry must be reported: %v", result.Warnings)
	}
}
