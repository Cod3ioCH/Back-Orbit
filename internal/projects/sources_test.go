package projects

import (
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

func detailWith(mounts ...docker.Mount) Detail {
	return Detail{
		DockerAvailable: true,
		Containers: []docker.Container{
			{Name: "app-1", Service: "app", Mounts: mounts},
		},
	}
}

// TestBindMountsAreOfferedAsSources is the gap this closes. A Compose file
// with `./data:/app/data` is at least as common as a named volume, and until
// now Back-Orbit told those projects they had nothing to back up — an answer,
// not an error, which is what made it dangerous.
func TestBindMountsAreOfferedAsSources(t *testing.T) {
	detail := detailWith(docker.Mount{
		Type:        "bind",
		Source:      "/Users/someone/app/data",
		Destination: "/app/data",
	})

	sources := BackupSources(detail)
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1: %+v", len(sources), sources)
	}

	source := sources[0]
	if source.Kind != SourceBind {
		t.Errorf("kind = %q, want bind", source.Kind)
	}
	if source.Name != "/Users/someone/app/data" {
		t.Errorf("name = %q, want the host path", source.Name)
	}
	if source.MountedAt != "/app/data" {
		t.Errorf("mountedAt = %q, want /app/data", source.MountedAt)
	}
	if !source.Backupable() {
		t.Errorf("an ordinary bind mount must be backupable, got skipped: %q", source.Skipped)
	}
}

// TestSystemMountsAreListedButSkipped: they must not be backed up, and they
// must not silently vanish either. A source that disappears without
// explanation is how someone ends up believing a directory is covered.
func TestSystemMountsAreListedButSkipped(t *testing.T) {
	cases := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
		"/etc/localtime",
		"/proc",
		"/sys/fs/cgroup",
		"/",
	}

	for _, hostPath := range cases {
		t.Run(hostPath, func(t *testing.T) {
			sources := BackupSources(detailWith(docker.Mount{
				Type:        "bind",
				Source:      hostPath,
				Destination: "/somewhere",
			}))

			if len(sources) != 1 {
				t.Fatalf("got %d sources, want the mount listed", len(sources))
			}
			if sources[0].Backupable() {
				t.Fatalf("%s must not be backed up", hostPath)
			}
			if sources[0].Skipped == "" {
				t.Error("a skipped source must say why, or it looks like an omission")
			}
		})
	}
}

func TestNamedVolumesAndBindsAppearTogether(t *testing.T) {
	detail := detailWith(
		docker.Mount{Type: "volume", Name: "app_data", Destination: "/var/lib/data"},
		docker.Mount{Type: "bind", Source: "/srv/uploads", Destination: "/app/uploads"},
	)
	detail.Volumes = []docker.Volume{{Name: "app_data"}}

	sources := BackupSources(detail)
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want both: %+v", len(sources), sources)
	}
	// Volumes sort first, so the order stays stable between refreshes.
	if sources[0].Kind != SourceVolume || sources[1].Kind != SourceBind {
		t.Errorf("unexpected order: %+v", sources)
	}
	if len(sources[0].Services) != 1 || sources[0].Services[0] != "app" {
		t.Errorf("the using service should be recorded, got %+v", sources[0].Services)
	}
}

// TestVolumeWithoutAContainerIsStillOffered covers a stopped project: the
// volume exists and holds data, and that is exactly when a backup matters.
func TestVolumeWithoutAContainerIsStillOffered(t *testing.T) {
	detail := Detail{
		DockerAvailable: true,
		Volumes:         []docker.Volume{{Name: "orphaned_data"}},
	}

	sources := BackupSources(detail)
	if len(sources) != 1 || sources[0].Name != "orphaned_data" {
		t.Fatalf("expected the volume to be offered, got %+v", sources)
	}
}
