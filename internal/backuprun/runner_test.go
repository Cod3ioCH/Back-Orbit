package backuprun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStagingPathIsStableAcrossRuns is the property that keeps successive
// backups of the same volume comparable. restic records the absolute path it
// backed up, so if this varied per run, retention grouped by path would file
// every run in a group of its own and never prune any of them — the repository
// would grow without limit while the UI reported retention as applied.
func TestStagingPathIsStableAcrossRuns(t *testing.T) {
	const root = "/var/lib/back-orbit/staging"

	first := filepath.Join(root, pathSegment("demo-projekt"), pathSegment("demo-projekt_daten"))
	second := filepath.Join(root, pathSegment("demo-projekt"), pathSegment("demo-projekt_daten"))

	if first != second {
		t.Fatalf("the same project and volume produced different paths:\n  %s\n  %s", first, second)
	}
	if strings.Contains(first, "staging/staging") {
		t.Errorf("unexpected path shape: %s", first)
	}
}

// TestPathSegmentCannotEscape covers names that reach Back-Orbit from Docker
// labels. They are not ours, so a separator or a traversal in one must not be
// able to move staging out of its directory.
func TestPathSegmentCannotEscape(t *testing.T) {
	const root = "/staging"

	hostile := []string{
		"../../etc",
		"..",
		".",
		"/absolute/path",
		"nested/child",
		`back\slash`,
		"",
		"...",
		"a/../../../b",
	}

	for _, name := range hostile {
		t.Run(name, func(t *testing.T) {
			segment := pathSegment(name)

			if segment == "" {
				t.Fatal("an empty segment would collapse the path")
			}
			if strings.ContainsAny(segment, `/\`) {
				t.Fatalf("segment %q still contains a separator", segment)
			}

			joined := filepath.Join(root, segment)
			if filepath.Clean(joined) != joined {
				t.Fatalf("segment %q does not survive cleaning: %s", segment, filepath.Clean(joined))
			}
			if !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
				t.Fatalf("segment %q escaped the staging root: %s", segment, joined)
			}
		})
	}
}

// TestPathSegmentKeepsNamesRecognisable: the path ends up in the snapshot and
// in front of the operator, so a name should still be readable after cleaning.
func TestPathSegmentKeepsNamesRecognisable(t *testing.T) {
	cases := map[string]string{
		"demo-projekt":            "demo-projekt",
		"demo-projekt_demo-daten": "demo-projekt_demo-daten",
		"My Project":              "My-Project",
	}

	for input, want := range cases {
		if got := pathSegment(input); got != want {
			t.Errorf("pathSegment(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStatusDone(t *testing.T) {
	if StatusRunning.Done() {
		t.Error("a running backup is not done")
	}
	for _, status := range []Status{StatusCompleted, StatusCompletedWithWarnings, StatusFailed, StatusCancelled} {
		if !status.Done() {
			t.Errorf("%s should count as done", status)
		}
	}
}
