// Package storage stages Docker data where the backup engine can read it.
//
// Named volumes live inside Docker's own storage, which a containerised
// Back-Orbit cannot reach directly. Staging bridges that gap: a short-lived
// helper container mounts the volume read-only, and its contents are streamed
// out through the Docker archive API.
//
// Nothing is ever executed in the helper container. The archive API can read a
// container's filesystem while it is merely "created", so the image's
// entrypoint never runs — there is no process to escape from, and staging
// needs no ability to run code on the host beyond creating and removing an
// inert container.
package storage

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
)

// mountPath is where the source volume appears inside the helper container.
const mountPath = "/back-orbit-source"

// OwnershipEntry records the original owner and mode of one staged path.
//
// This exists because of a fidelity problem that would otherwise be silent.
// Back-Orbit runs as an unprivileged user, so while extracting it cannot
// restore a file's original uid/gid — those calls fail with EPERM. The backup
// engine then records Back-Orbit's own uid instead, and a restore would hand
// an application files it does not own. Capturing the real values from the tar
// headers keeps the information, so a restore can reapply it.
type OwnershipEntry struct {
	Path string `json:"path"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
	Mode uint32 `json:"mode"`
}

// Result describes what a staging run produced.
type Result struct {
	// Dir is the directory the volume contents were written to.
	Dir string
	// Files is the number of entries extracted.
	Files int
	// Bytes is the total size of the regular files extracted.
	Bytes int64
	// Ownership holds the original uid/gid/mode of every entry.
	Ownership []OwnershipEntry
	// OwnershipPreserved reports whether the staged files on disk actually
	// carry their original ownership. When false, the Ownership list is the
	// only record of it and a restore must reapply it.
	OwnershipPreserved bool
	// SQLiteDatabases lists databases that were re-taken consistently rather
	// than left as the plain file copy. Recorded so a restore — and the person
	// reading the snapshot later — can tell which files are trustworthy.
	SQLiteDatabases []SQLiteCapture
	// Warnings describes anything that could not be staged faithfully.
	Warnings []string
	// Duration is how long staging took.
	Duration time.Duration
}

// Stager stages Docker volumes into a directory tree.
type Stager struct {
	docker docker.Client
	// helperImage is the image helper containers are created from. Empty
	// means "discover Back-Orbit's own image", which is guaranteed to exist
	// locally and so never requires a pull.
	helperImage string
}

// NewStager creates a Stager. helperImage may be empty.
func NewStager(dockerClient docker.Client, helperImage string) *Stager {
	return &Stager{docker: dockerClient, helperImage: helperImage}
}

// StageVolume copies the contents of a named volume into destDir.
func (s *Stager) StageVolume(ctx context.Context, volumeName, destDir string) (*Result, error) {
	if volumeName == "" {
		return nil, errors.New("storage: volume name must not be empty")
	}
	return s.stage(ctx, volumeName, destDir, "stage-volume:"+volumeName)
}

// StageBindMount copies the contents of a host directory into destDir.
//
// Back-Orbit cannot see the host filesystem, but the Docker daemon can: the
// helper container names the host path as its bind source and the daemon
// resolves it. Without this, the most common way people persist data — a
// `./data:/app/data` line in a Compose file — could not be backed up at all.
func (s *Stager) StageBindMount(ctx context.Context, hostPath, destDir string) (*Result, error) {
	if hostPath == "" {
		return nil, errors.New("storage: bind mount path must not be empty")
	}
	if !strings.HasPrefix(hostPath, "/") {
		return nil, fmt.Errorf("storage: bind mount source %q must be an absolute path", hostPath)
	}
	return s.stage(ctx, hostPath, destDir, "stage-bind:"+hostPath)
}

// stage reads a source — a named volume or a host path — into destDir.
//
// The helper container is always removed, including when staging fails part
// way through, so a failed backup cannot leave containers behind.
func (s *Stager) stage(ctx context.Context, source, destDir, purpose string) (*Result, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: create staging directory: %w", err)
	}

	image, err := s.resolveHelperImage(ctx)
	if err != nil {
		return nil, err
	}

	started := time.Now()

	containerID, err := s.docker.CreateHelperContainer(ctx, docker.HelperContainerRequest{
		Image:     image,
		Source:    source,
		MountPath: mountPath,
		Purpose:   purpose,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create helper container for %q: %w", source, err)
	}

	// Removal runs on every path out of this function, including panics.
	// A leaked helper container pins the source and confuses the next run, so
	// this must not depend on reaching the happy path.
	defer func() {
		// A fresh context: the caller's may already be cancelled, and cleanup
		// still has to happen.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if removeErr := s.docker.RemoveContainer(cleanupCtx, containerID); removeErr != nil {
			slog.Error("storage: could not remove helper container; it will be swept on next start",
				"container", containerID, "source", source, "error", removeErr)
		}
	}()

	archive, err := s.docker.ContainerArchive(ctx, containerID, mountPath+"/.")
	if err != nil {
		return nil, fmt.Errorf("storage: read %q: %w", source, err)
	}
	defer archive.Close()

	result, err := extractTar(archive, destDir)
	if err != nil {
		return nil, fmt.Errorf("storage: extract %q: %w", source, err)
	}

	// A plain file copy of a live SQLite database is not a backup of it, so any
	// database found in what was just staged is re-taken through SQLite itself.
	// This happens after the copy rather than instead of it: the copy gives the
	// complete tree, and only the database files within it are replaced.
	captures, sqliteWarnings, err := s.captureSQLiteDatabases(ctx, source, destDir)
	if err != nil {
		return nil, err
	}
	result.SQLiteDatabases = captures
	result.Warnings = append(result.Warnings, sqliteWarnings...)

	// The captured databases differ in size from what was first copied, so the
	// totals are recomputed rather than left describing a tree that no longer
	// exists.
	if len(captures) > 0 {
		if err := recountTree(destDir, result); err != nil {
			return nil, err
		}
	}

	result.Dir = destDir
	result.Duration = time.Since(started)
	return result, nil
}

// recountTree refreshes the file and byte totals after the tree was modified.
func recountTree(root string, result *Result) error {
	var files int
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		files++
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			bytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("storage: recount staged tree: %w", err)
	}
	result.Files = files
	result.Bytes = bytes
	return nil
}

// SweepOrphans removes helper containers left behind by an earlier run that
// died before it could clean up. Called at startup, it keeps a crash from
// leaking containers indefinitely.
func (s *Stager) SweepOrphans(ctx context.Context) (int, error) {
	ids, err := s.docker.ListHelperContainers(ctx)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, id := range ids {
		if err := s.docker.RemoveContainer(ctx, id); err != nil {
			slog.Error("storage: could not remove orphaned helper container", "container", id, "error", err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (s *Stager) resolveHelperImage(ctx context.Context) (string, error) {
	if s.helperImage != "" {
		return s.helperImage, nil
	}
	image, err := s.docker.SelfImage(ctx)
	if err != nil {
		return "", fmt.Errorf("storage: no helper image configured and Back-Orbit's own image "+
			"could not be determined: %w", err)
	}
	return image, nil
}

// extractTar writes a tar stream into destDir.
//
// Every entry's path is checked before anything is written. A tar is untrusted
// input here — it comes from whatever is inside a user's volume — and an entry
// named "../../etc/passwd" or an absolute path would otherwise write outside
// the staging directory entirely.
func extractTar(r io.Reader, destDir string) (*Result, error) {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return nil, err
	}

	result := &Result{OwnershipPreserved: true}
	reader := tar.NewReader(r)

	// Directory modes and times are applied after all contents are written,
	// because writing into a directory updates its mtime and a read-only mode
	// would stop the writes.
	type pendingDir struct {
		path    string
		mode    os.FileMode
		modTime time.Time
	}
	var dirs []pendingDir

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		target, err := safeJoin(root, header.Name)
		if err != nil {
			// Refuse the whole archive rather than skipping the entry: a tar
			// trying to escape its destination is not a partial-success case.
			return nil, err
		}
		if target == root {
			continue // the archive's own root entry
		}

		result.Ownership = append(result.Ownership, OwnershipEntry{
			Path: relativeTo(root, target),
			UID:  header.Uid,
			GID:  header.Gid,
			Mode: uint32(header.Mode),
		})

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return nil, fmt.Errorf("create directory %s: %w", header.Name, err)
			}
			dirs = append(dirs, pendingDir{
				path:    target,
				mode:    header.FileInfo().Mode().Perm(),
				modTime: header.ModTime,
			})

		case tar.TypeReg:
			written, err := writeFile(target, reader, header)
			if err != nil {
				return nil, err
			}
			result.Bytes += written

		case tar.TypeSymlink:
			// The link target is not resolved or validated: a symlink is data,
			// and rewriting it would corrupt the backup. It is only ever
			// followed at restore time, where the restore path is responsible
			// for refusing to write through one that escapes its target.
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return nil, err
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return nil, fmt.Errorf("create symlink %s: %w", header.Name, err)
			}

		case tar.TypeLink:
			source, err := safeJoin(root, header.Linkname)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return nil, err
			}
			_ = os.Remove(target)
			if err := os.Link(source, target); err != nil {
				return nil, fmt.Errorf("create hard link %s: %w", header.Name, err)
			}

		default:
			// Devices, sockets and FIFOs cannot be recreated without
			// privileges and carry no data worth backing up. Skipping them is
			// correct, but it must be said out loud rather than silently.
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("skipped %s: unsupported entry type", header.Name))
			continue
		}

		result.Files++

		// Ownership is attempted for every entry. Running unprivileged this
		// fails, which is expected and recorded once rather than per file.
		if err := os.Lchown(target, header.Uid, header.Gid); err != nil && result.OwnershipPreserved {
			result.OwnershipPreserved = false
			result.Warnings = append(result.Warnings,
				"the staged copy uses Back-Orbit's UID/GID because the original file ownership "+
					"cannot be applied by the unprivileged container; original UID/GID values are "+
					"recorded in the snapshot manifest and must be reapplied during restore")
		}
	}

	// Apply directory metadata last, deepest first.
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Chtimes(dirs[i].path, dirs[i].modTime, dirs[i].modTime)
		if err := os.Chmod(dirs[i].path, dirs[i].mode); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not set permissions on %s: %v", relativeTo(root, dirs[i].path), err))
		}
	}

	return result, nil
}

func writeFile(target string, reader io.Reader, header *tar.Header) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return 0, err
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode().Perm())
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", header.Name, err)
	}

	written, err := io.Copy(file, reader)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return written, fmt.Errorf("write %s: %w", header.Name, err)
	}

	// Set the mode explicitly: OpenFile's permissions are subject to umask.
	if err := os.Chmod(target, header.FileInfo().Mode().Perm()); err != nil {
		return written, fmt.Errorf("set permissions on %s: %w", header.Name, err)
	}
	if err := os.Chtimes(target, header.ModTime, header.ModTime); err != nil {
		return written, fmt.Errorf("set times on %s: %w", header.Name, err)
	}

	return written, nil
}

// safeJoin resolves an archive entry name against root and refuses anything
// that would land outside it.
//
// Traversal is rejected rather than normalised away. Silently turning
// "../escaped.txt" into "escaped.txt" would keep the extraction contained, but
// it would also file the entry under a path the volume never had — and a
// backup that quietly relocates data is not a faithful one. Docker's archive
// API never produces such names, so encountering one means something is wrong
// enough to stop for.
func safeJoin(root, name string) (string, error) {
	relative := strings.TrimPrefix(filepath.Clean(filepath.FromSlash(name)), string(os.PathSeparator))

	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: refusing archive entry %q: it would be written outside the staging directory", name)
	}

	target := filepath.Join(root, relative)

	// Belt and braces: compare against root plus a separator, so a sibling
	// directory whose name merely starts with root ("/stage-evil" vs
	// "/stage") is never accepted.
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: refusing archive entry %q: it would be written outside the staging directory", name)
	}
	return target, nil
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
