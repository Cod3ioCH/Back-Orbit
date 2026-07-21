package repositories

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotARepository is returned when a location holds data that is not a
// restic repository. Deleting it would destroy something Back-Orbit never
// created and cannot describe.
var ErrNotARepository = errors.New("repositories: the location is not a restic repository")

// purgeLocalRepository permanently deletes the restic repository at path.
//
// This is the most destructive thing Back-Orbit can do, so it refuses to act
// on anything it cannot positively identify. A mistyped or later-edited path
// would otherwise turn "remove this repository" into "erase that directory",
// and the snapshots it wipes are exactly the copy that exists for the day
// everything else is gone.
//
// The guards, in order:
//
//   - Symlinks are resolved first, so a link anywhere along the path cannot
//     redirect the deletion to a directory that was never configured.
//   - The target must be a directory.
//   - It must carry restic's own signature — a "config" file and a "keys"
//     directory. An empty directory is accepted, since there is nothing to
//     lose; anything else is refused.
//
// Reports whether anything was actually removed: a repository that was
// configured but never initialised leaves nothing behind, and saying "deleted"
// about it would be a lie.
func purgeLocalRepository(path string) (bool, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Never initialised, or already gone. Both mean there is no data.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("repositories: resolve %q: %w", path, err)
	}

	info, err := os.Lstat(resolved)
	if err != nil {
		return false, fmt.Errorf("repositories: inspect %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: %s is not a directory", ErrNotARepository, resolved)
	}

	empty, err := isEmptyDir(resolved)
	if err != nil {
		return false, err
	}
	if !empty {
		if err := verifyResticLayout(resolved); err != nil {
			return false, err
		}
	}

	if err := os.RemoveAll(resolved); err != nil {
		return false, fmt.Errorf("repositories: delete %q: %w", resolved, err)
	}
	return !empty, nil
}

// verifyResticLayout checks for the files restic itself creates, so a
// directory holding anything else is left alone.
func verifyResticLayout(dir string) error {
	config, err := os.Stat(filepath.Join(dir, "config"))
	if err != nil || config.IsDir() {
		return fmt.Errorf("%w: %s has no restic config file, so it holds data Back-Orbit "+
			"did not create; delete it yourself if that is what you intend", ErrNotARepository, dir)
	}

	keys, err := os.Stat(filepath.Join(dir, "keys"))
	if err != nil || !keys.IsDir() {
		return fmt.Errorf("%w: %s has no restic keys directory, so it holds data Back-Orbit "+
			"did not create; delete it yourself if that is what you intend", ErrNotARepository, dir)
	}

	return nil
}

func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("repositories: read %q: %w", dir, err)
	}
	return len(entries) == 0, nil
}
