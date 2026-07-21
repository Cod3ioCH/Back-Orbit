package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests run the real restic binary against a temporary local
// repository. Mocking the engine would only prove that the wrapper calls the
// functions it was written to call; for a backup tool the question that
// matters is whether data actually comes back out, and only the real binary
// can answer that. Tests skip when restic is unavailable so the suite still
// runs on machines without it.
func requireRestic(t *testing.T) *ResticEngine {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not installed; skipping engine integration test")
	}
	return NewResticEngine("")
}

const testPassword = "test-repository-password-not-a-real-secret"

// newRepository creates an initialised repository in a temp dir.
func newRepository(t *testing.T, engine *ResticEngine) RepositoryConfig {
	t.Helper()
	config := RepositoryConfig{
		Kind:     RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "repo"),
		Password: testPassword,
	}
	if err := engine.InitRepository(context.Background(), config); err != nil {
		t.Fatalf("InitRepository: %v", err)
	}
	return config
}

// writeTree creates a small directory tree to back up and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func TestEngineAvailability(t *testing.T) {
	engine := requireRestic(t)
	if err := engine.EnsureAvailable(context.Background()); err != nil {
		t.Fatalf("EnsureAvailable: %v", err)
	}
}

func TestMissingBinaryIsClassified(t *testing.T) {
	engine := NewResticEngine("definitely-not-a-real-restic-binary")
	err := engine.EnsureAvailable(context.Background())
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
	if KindOf(err) != ErrKindEngineMissing {
		t.Fatalf("expected ErrKindEngineMissing, got %q", KindOf(err))
	}
}

// TestBackupRestoreRoundTrip is the test that matters most: data written,
// backed up, and restored must be byte-identical. A backup tool that cannot
// prove this has proven nothing.
func TestBackupRestoreRoundTrip(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{
		"config.yml":       "answer: 42\n",
		"nested/data.txt":  "hello from a nested file",
		"nested/empty.txt": "",
	})

	var sawProgress bool
	result, err := engine.CreateSnapshot(ctx, SnapshotRequest{
		Repository: repo,
		Paths:      []string{source},
		Tags:       []string{"project:test", "plan:roundtrip"},
		Host:       "back-orbit-test",
		OnProgress: func(Progress) { sawProgress = true },
	})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if result.SnapshotID == "" {
		t.Fatal("expected a snapshot id")
	}
	if result.TotalFiles == 0 {
		t.Fatal("expected the summary to report processed files")
	}
	_ = sawProgress // progress is timing-dependent on tiny trees; not asserted

	// The snapshot must be listed with the metadata we attached.
	snapshots, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected exactly 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Host != "back-orbit-test" {
		t.Fatalf("expected host to be preserved, got %q", snapshots[0].Host)
	}
	if len(snapshots[0].Tags) != 2 {
		t.Fatalf("expected the tags to be preserved, got %v", snapshots[0].Tags)
	}

	// Restore into a fresh directory and compare every file.
	target := t.TempDir()
	restore, err := engine.RestoreSnapshot(ctx, RestoreRequest{
		Repository: repo,
		SnapshotID: result.SnapshotID,
		TargetPath: target,
	})
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if restore.FilesRestored == 0 {
		t.Fatal("expected the restore summary to report restored files")
	}

	for name, want := range map[string]string{
		"config.yml":       "answer: 42\n",
		"nested/data.txt":  "hello from a nested file",
		"nested/empty.txt": "",
	} {
		// restic restores the absolute source path underneath the target.
		got, err := os.ReadFile(filepath.Join(target, source, name))
		if err != nil {
			t.Fatalf("restored file %s missing: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("restored %s = %q, want %q", name, got, want)
		}
	}
}

func TestVerifyRepositoryReportsHealthy(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{"a.txt": "content"})
	if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{
		Repository: repo,
		Paths:      []string{source},
	}); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	result, err := engine.VerifyRepository(ctx, repo)
	if err != nil {
		t.Fatalf("VerifyRepository: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected a healthy repository, got errors: %v", result.Errors)
	}
}

func TestWrongPasswordIsClassified(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	wrong := repo
	wrong.Password = "this-is-not-the-password"

	_, err := engine.ListSnapshots(ctx, wrong)
	if err == nil {
		t.Fatal("expected an error when using the wrong password")
	}
	if kind := KindOf(err); kind != ErrKindWrongPassword {
		t.Fatalf("expected ErrKindWrongPassword, got %q (err: %v)", kind, err)
	}
	if KindOf(err).Retryable() {
		t.Fatal("a wrong password must not be classified as retryable")
	}
}

func TestMissingRepositoryIsClassified(t *testing.T) {
	engine := requireRestic(t)

	missing := RepositoryConfig{
		Kind:     RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "does-not-exist"),
		Password: testPassword,
	}

	_, err := engine.ListSnapshots(context.Background(), missing)
	if err == nil {
		t.Fatal("expected an error for a repository that does not exist")
	}
	if kind := KindOf(err); kind != ErrKindRepositoryNotFound {
		t.Fatalf("expected ErrKindRepositoryNotFound, got %q (err: %v)", kind, err)
	}
}

func TestInitOnExistingRepositoryIsClassified(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	err := engine.InitRepository(ctx, repo)
	if err == nil {
		t.Fatal("expected an error re-initialising an existing repository")
	}
	if kind := KindOf(err); kind != ErrKindRepositoryExists {
		t.Fatalf("expected ErrKindRepositoryExists, got %q (err: %v)", kind, err)
	}
}

func TestRetentionKeepsRequestedSnapshots(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	// Three snapshots of changing content, so each is distinct.
	for i, content := range []string{"one", "two", "three"} {
		source := writeTree(t, map[string]string{"file.txt": content})
		if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{
			Repository: repo,
			Paths:      []string{source},
			Host:       "retention-test",
		}); err != nil {
			t.Fatalf("CreateSnapshot %d: %v", i, err)
		}
	}

	before, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("expected 3 snapshots before retention, got %d", len(before))
	}

	if err := engine.ApplyRetention(ctx, repo, RetentionPolicy{KeepLast: 1}); err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}

	after, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots after retention: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("expected 1 snapshot after keep-last=1, got %d", len(after))
	}
}

// TestRetentionCountsAcrossDifferingPaths guards the grouping trap: restic's
// own `forget` default buckets snapshots by host and paths, so "keep the last
// one" would keep one *per path set*. A plan whose selected paths change would
// leave its earlier snapshots in a bucket of their own that retention never
// touches — the repository grows forever while the UI reports retention as
// applied. The engine therefore always states the grouping explicitly.
func TestRetentionCountsAcrossDifferingPaths(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	// Each snapshot deliberately backs up a *different* path, which is what
	// restic's default grouping would split apart.
	for _, content := range []string{"one", "two", "three"} {
		source := writeTree(t, map[string]string{"file.txt": content})
		if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{
			Repository: repo,
			Paths:      []string{source},
			Host:       "grouping-test",
		}); err != nil {
			t.Fatalf("CreateSnapshot: %v", err)
		}
	}

	if err := engine.ApplyRetention(ctx, repo, RetentionPolicy{KeepLast: 1}); err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}

	after, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("keep-last=1 must keep exactly one snapshot in total, got %d — "+
			"grouping is not being applied explicitly", len(after))
	}
}

// TestRetentionOnlyTouchesItsOwnSnapshots proves a plan cannot delete another
// plan's snapshots when they share a repository.
func TestRetentionOnlyTouchesItsOwnSnapshots(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{"file.txt": "data"})
	for _, tag := range []string{"plan:a", "plan:a", "plan:b"} {
		if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{
			Repository: repo,
			Paths:      []string{source},
			Tags:       []string{tag},
			Host:       "multi-plan",
		}); err != nil {
			t.Fatalf("CreateSnapshot(%s): %v", tag, err)
		}
	}

	// Prune plan A down to one snapshot; plan B must be untouched.
	if err := engine.ApplyRetention(ctx, repo, RetentionPolicy{
		KeepLast: 1,
		OnlyTags: []string{"plan:a"},
	}); err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}

	after, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}

	var planA, planB int
	for _, snapshot := range after {
		for _, tag := range snapshot.Tags {
			switch tag {
			case "plan:a":
				planA++
			case "plan:b":
				planB++
			}
		}
	}
	if planA != 1 {
		t.Fatalf("expected 1 snapshot left for plan:a, got %d", planA)
	}
	if planB != 1 {
		t.Fatalf("plan:b snapshots must be untouched, got %d", planB)
	}
}

// TestEmptyRetentionPolicyIsRefused guards a destructive edge case: a policy
// with no keep rules would tell restic to keep nothing. That is far more
// likely to be a misconfiguration than a genuine request to delete every
// snapshot, so the engine must refuse rather than obey.
func TestEmptyRetentionPolicyIsRefused(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{"file.txt": "keep me"})
	if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}}); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	if err := engine.ApplyRetention(ctx, repo, RetentionPolicy{}); err == nil {
		t.Fatal("expected an empty retention policy to be refused")
	}

	snapshots, err := engine.ListSnapshots(ctx, repo)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("the snapshot must survive a refused policy, got %d snapshots", len(snapshots))
	}
}

func TestPruneRepository(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{"file.txt": "data"})
	if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}}); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if err := engine.PruneRepository(ctx, repo); err != nil {
		t.Fatalf("PruneRepository: %v", err)
	}
}

// TestCancellationStopsBackup proves a running backup is abortable, which the
// job system depends on for its cancel action.
func TestCancellationStopsBackup(t *testing.T) {
	engine := requireRestic(t)
	repo := newRepository(t, engine)
	source := writeTree(t, map[string]string{"file.txt": "data"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the run starts

	_, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}})
	if err == nil {
		t.Fatal("expected a cancelled backup to fail")
	}
	if kind := KindOf(err); kind != ErrKindCancelled {
		t.Fatalf("expected ErrKindCancelled, got %q (err: %v)", kind, err)
	}
}

func TestCreateSnapshotRejectsEmptyPaths(t *testing.T) {
	engine := requireRestic(t)
	repo := newRepository(t, engine)

	if _, err := engine.CreateSnapshot(context.Background(), SnapshotRequest{Repository: repo}); err == nil {
		t.Fatal("expected an error when no paths are given")
	}
}

// TestSecretsNeverReachErrorOutput checks the redaction promise end to end:
// force a failure with a real repository password in play and confirm the
// password cannot be found anywhere in the resulting error.
func TestSecretsNeverReachErrorOutput(t *testing.T) {
	engine := requireRestic(t)

	const secret = "super-secret-repository-password-12345"
	missing := RepositoryConfig{
		Kind:     RepositoryLocal,
		Location: filepath.Join(t.TempDir(), "nope"),
		Password: secret,
	}

	_, err := engine.ListSnapshots(context.Background(), missing)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the repository password leaked into an error message: %v", err)
	}
}

func TestOperationsRespectContextDeadline(t *testing.T) {
	engine := requireRestic(t)
	repo := newRepository(t, engine)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	if _, err := engine.ListSnapshots(ctx, repo); err == nil {
		t.Fatal("expected the deadline to abort the operation")
	}
}

// TestVerifySnapshotConfirmsTheBackupIsThere is the check that turns "restic
// reported success" into "the backup exists and can be read back".
func TestVerifySnapshotConfirmsTheBackupIsThere(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{
		"a.txt":     "content",
		"sub/b.txt": "more content",
	})
	snapshot, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	result, err := engine.VerifySnapshot(ctx, repo, snapshot.SnapshotID)
	if err != nil {
		t.Fatalf("VerifySnapshot: %v", err)
	}
	if !result.OK {
		t.Fatalf("a snapshot just written must verify, got: %v", result.Errors)
	}
	if result.FilesListed == 0 {
		t.Error("the snapshot's tree resolved to nothing, which cannot be right for two files")
	}
	// What was checked has to be stated, so nothing downstream can claim the
	// data blobs were re-read when they were not.
	if len(result.Checks) < 2 {
		t.Errorf("expected both the structure and the snapshot to be named as checked, got %v", result.Checks)
	}
}

// TestVerifySnapshotFailsForAMissingSnapshot is the case that matters: a
// repository can be perfectly healthy and still not contain the backup someone
// believes they took.
func TestVerifySnapshotFailsForAMissingSnapshot(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	source := writeTree(t, map[string]string{"a.txt": "content"})
	if _, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}}); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// The repository itself is sound...
	structure, err := engine.VerifyRepository(ctx, repo)
	if err != nil || !structure.OK {
		t.Fatalf("the repository should be healthy: %v %v", structure, err)
	}

	// ...but this snapshot is not in it.
	absent := "0000000000000000000000000000000000000000000000000000000000000000"
	result, err := engine.VerifySnapshot(ctx, repo, absent)
	if err != nil {
		t.Fatalf("a missing snapshot is a verification result, not an engine failure: %v", err)
	}
	if result.OK {
		t.Fatal("verification passed for a snapshot that does not exist")
	}
	if len(result.Errors) == 0 {
		t.Error("a failed verification must say what was wrong")
	}
}

func TestVerifySnapshotRejectsAnEmptyID(t *testing.T) {
	engine := requireRestic(t)
	repo := newRepository(t, engine)

	if _, err := engine.VerifySnapshot(context.Background(), repo, ""); err == nil {
		t.Fatal("verifying without a snapshot id must fail rather than check nothing and pass")
	}
}

// TestVerifySnapshotCountsAccuratelyOnLargeSnapshots guards the mistake that
// listing the snapshot made: the engine bounds captured output, and a listing
// of any real volume runs to hundreds of kilobytes, so the count came back
// truncated and far too low. A verification reporting a confidently wrong
// number is worse than one reporting none, because it looks checked.
func TestVerifySnapshotCountsAccuratelyOnLargeSnapshots(t *testing.T) {
	engine := requireRestic(t)
	ctx := context.Background()
	repo := newRepository(t, engine)

	// Enough entries that a per-entry listing would exceed maxCapturedOutput
	// several times over.
	files := map[string]string{}
	for i := 0; i < 600; i++ {
		files[fmt.Sprintf("dir%02d/file-with-a-reasonably-long-name-%04d.txt", i%20, i)] = "x"
	}
	source := writeTree(t, files)

	snapshot, err := engine.CreateSnapshot(ctx, SnapshotRequest{Repository: repo, Paths: []string{source}})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	result, err := engine.VerifySnapshot(ctx, repo, snapshot.SnapshotID)
	if err != nil {
		t.Fatalf("VerifySnapshot: %v", err)
	}
	if !result.OK {
		t.Fatalf("verification failed: %v", result.Errors)
	}

	// The count must reflect the whole snapshot, not as much of it as fitted
	// in a buffer.
	if result.FilesListed < 600 {
		t.Errorf("FilesListed = %d, want at least the 600 files written — the count is truncated",
			result.FilesListed)
	}
	if result.BytesInSnapshot < 600 {
		t.Errorf("BytesInSnapshot = %d, want at least 600", result.BytesInSnapshot)
	}
}
