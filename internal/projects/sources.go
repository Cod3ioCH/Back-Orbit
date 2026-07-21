package projects

import (
	"path"
	"sort"
	"strings"
)

// SourceKind distinguishes the two ways a Compose project keeps data.
type SourceKind string

const (
	// SourceVolume is a Docker named volume.
	SourceVolume SourceKind = "volume"
	// SourceBind is a host directory mounted into a container.
	SourceBind SourceKind = "bind"
)

// Source is one thing worth backing up in a project.
//
// Both kinds are listed together on purpose. A named volume is the tidier
// pattern, but a bind mount — `./data:/app/data` in a Compose file — is at
// least as common in practice, and a backup tool that only sees the tidy half
// tells people with the other half that they have nothing to back up. That is
// worse than an error: it looks like an answer.
type BackupSource struct {
	Kind SourceKind `json:"kind"`
	// Name identifies the source: the volume name, or the host path.
	Name string `json:"name"`
	// MountedAt is where the data appears inside the application's container,
	// which is how someone recognises what a source actually holds.
	MountedAt string `json:"mountedAt"`
	// Services lists the containers using it.
	Services []string `json:"services"`
	// Skipped explains why a source is present but will not be backed up,
	// rather than leaving it silently absent from the list.
	Skipped string `json:"skipped,omitempty"`
}

// Backupable reports whether this source will actually be backed up.
func (s BackupSource) Backupable() bool { return s.Skipped == "" }

// unbackupablePaths are host paths that are mounted for access, not for data.
// Backing them up would capture the host's own system state, which is not what
// anyone asked for and in the socket's case is a security problem.
var unbackupablePaths = map[string]string{
	"/var/run/docker.sock": "this is the Docker socket, not application data",
	"/run/docker.sock":     "this is the Docker socket, not application data",
	"/etc/localtime":       "this is a host system file, not application data",
	"/etc/timezone":        "this is a host system file, not application data",
}

// BackupSources returns everything in a project that holds data, from its live
// Docker state.
func BackupSources(detail Detail) []BackupSource {
	byName := map[string]*BackupSource{}

	for _, volume := range detail.Volumes {
		byName[volume.Name] = &BackupSource{
			Kind: SourceVolume,
			Name: volume.Name,
		}
	}

	for _, container := range detail.Containers {
		service := container.Service
		if service == "" {
			service = container.Name
		}

		for _, mount := range container.Mounts {
			switch mount.Type {
			case "volume":
				if mount.Name == "" {
					continue
				}
				source, ok := byName[mount.Name]
				if !ok {
					source = &BackupSource{Kind: SourceVolume, Name: mount.Name}
					byName[mount.Name] = source
				}
				source.MountedAt = mount.Destination
				source.Services = appendUnique(source.Services, service)

			case "bind":
				if mount.Source == "" || !strings.HasPrefix(mount.Source, "/") {
					continue
				}
				source, ok := byName[mount.Source]
				if !ok {
					source = &BackupSource{
						Kind:    SourceBind,
						Name:    mount.Source,
						Skipped: skipReason(mount.Source),
					}
					byName[mount.Source] = source
				}
				source.MountedAt = mount.Destination
				source.Services = appendUnique(source.Services, service)
			}
		}
	}

	sources := make([]BackupSource, 0, len(byName))
	for _, source := range byName {
		sources = append(sources, *source)
	}

	// Volumes first, then binds, each alphabetically: a stable order so the
	// list does not reshuffle between refreshes.
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Kind != sources[j].Kind {
			return sources[i].Kind == SourceVolume
		}
		return sources[i].Name < sources[j].Name
	})
	return sources
}

func skipReason(hostPath string) string {
	clean := path.Clean(hostPath)
	if clean == "/" {
		return "this is the host's root directory"
	}
	if reason, found := unbackupablePaths[clean]; found {
		return reason
	}
	for _, prefix := range []string{"/proc", "/sys", "/dev"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return "this is a kernel filesystem, not application data"
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
