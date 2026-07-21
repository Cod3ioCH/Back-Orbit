package repositories

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Location describes a directory that a local repository could live in, and
// whether Back-Orbit can actually write there.
//
// This exists because "where do I put a local backup?" has no discoverable
// answer from inside a container. Back-Orbit runs unprivileged and sees only
// the paths its deployment mounted, so an operator staring at an empty text
// field has no way to know which of them are writable. Reporting the answer is
// the difference between a two-second decision and a guessing game that ends
// in a failed backup.
type Location struct {
	Path string `json:"path"`
	// Label names the location in the UI.
	Label string `json:"label"`
	// Description explains what the location is and where it survives to.
	Description string `json:"description"`
	// Writable reports whether Back-Orbit can create files here right now.
	Writable bool `json:"writable"`
	// Detail explains a false Writable in terms the operator can act on.
	Detail string `json:"detail,omitempty"`
	// Recommended marks the location to offer by default.
	Recommended bool `json:"recommended"`
}

// Locations answers questions about where local repositories may be stored.
type Locations struct {
	// dataDir is Back-Orbit's own state directory. It is tracked in order to
	// keep repositories *out* of it, not to offer it.
	dataDir string
	// backupDir is the mount point deployments provide for backups.
	backupDir string
}

// NewLocations creates a Locations for the given directories.
func NewLocations(dataDir, backupDir string) *Locations {
	return &Locations{dataDir: cleanAbs(dataDir), backupDir: cleanAbs(backupDir)}
}

// Suggest returns the local destinations worth offering, each with its real,
// probed writability rather than an assumption about it.
func (l *Locations) Suggest() []Location {
	if l.backupDir == "" {
		return nil
	}

	location := Location{
		Path:        l.backupDir,
		Label:       "Backup volume",
		Description: "Provided by your deployment for exactly this purpose, and kept separate from Back-Orbit's own database.",
		Recommended: true,
	}

	if probed, err := probeWritable(l.backupDir); err != nil {
		location.Detail = describeUnwritable(probed, err)
	} else {
		location.Writable = true
	}

	return []Location{location}
}

// validateLocalPath decides whether a local repository may live at path.
//
// Two things are checked, and both exist because of failures that are
// otherwise only discovered later — one at the next backup, the other on the
// day the backup is needed.
func (l *Locations) validateLocalPath(path string) error {
	if err := l.refuseInsideDataDir(path); err != nil {
		return err
	}
	path = cleanAbs(path)

	// Writability is probed, not inferred from permission bits: bits ignore
	// read-only mounts, supplementary groups and ACLs, and getting this wrong
	// means accepting a configuration whose first backup is guaranteed to fail.
	if probed, err := probeWritable(path); err != nil {
		return fmt.Errorf("%w: %s. %s",
			ErrInvalidConfig, describeUnwritable(probed, err), l.alternativeHint())
	}

	return nil
}

// refuseInsideDataDir rejects a location that overlaps Back-Orbit's own data
// directory, in either direction.
//
// A repository inside the data directory shares the fate of the database: one
// lost volume takes the application state and every backup with it, which is
// the single failure a backup tool exists to prevent. A repository that
// *contains* the data directory has the mirror problem — it pulls the database
// into the backup destination. Both are refused rather than warned about, and
// the operator is told where to go instead.
//
// Checked when a repository is created, and again before its data is deleted:
// there the consequence of being wrong cannot be undone.
func (l *Locations) refuseInsideDataDir(path string) error {
	if l.dataDir == "" {
		return nil
	}
	path = cleanAbs(path)

	if withinOrEqual(path, l.dataDir) {
		return fmt.Errorf("%w: %s is inside Back-Orbit's own data directory (%s). "+
			"Backups stored there would be lost by the same failure that loses Back-Orbit's "+
			"database, so they would not protect anything. %s",
			ErrInvalidConfig, path, l.dataDir, l.alternativeHint())
	}
	if withinOrEqual(l.dataDir, path) {
		return fmt.Errorf("%w: %s contains Back-Orbit's own data directory (%s), "+
			"which would mix the application's database into the backup destination. %s",
			ErrInvalidConfig, path, l.dataDir, l.alternativeHint())
	}
	return nil
}

func (l *Locations) alternativeHint() string {
	if l.backupDir == "" {
		return "Mount a writable directory into the container and use a path inside it."
	}
	return fmt.Sprintf("Use a path inside %s, which your deployment provides for backups.", l.backupDir)
}

// probeWritable reports whether Back-Orbit can create a repository at path.
//
// It writes and removes a temporary file rather than reading permission bits,
// because only an actual write accounts for read-only mounts, ACLs and the
// process's supplementary groups. When path does not exist yet the nearest
// existing ancestor is probed instead, since `restic init` will create the
// leaf — nothing here creates any directory, so checking a location never has
// a side effect on storage.
//
// The directory actually probed is returned alongside the error, because when
// path does not exist the failure belongs to an ancestor, and naming the wrong
// one sends the operator to fix a directory that was never the problem.
func probeWritable(path string) (string, error) {
	target, err := nearestExisting(path)
	if err != nil {
		return path, err
	}

	info, err := os.Stat(target)
	if err != nil {
		return target, err
	}
	if !info.IsDir() {
		return target, errNotADirectory
	}

	file, err := os.CreateTemp(target, ".back-orbit-write-test-*")
	if err != nil {
		return target, err
	}
	name := file.Name()
	_ = file.Close()
	if err := os.Remove(name); err != nil {
		return target, err
	}
	return target, nil
}

var errNotADirectory = errors.New("path exists but is not a directory")

// nearestExisting walks up from path until it finds something that exists.
func nearestExisting(path string) (string, error) {
	current := path
	for {
		if _, err := os.Stat(current); err == nil {
			return current, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fs.ErrNotExist
		}
		current = parent
	}
}

// describeUnwritable turns a probe failure into something an operator can act
// on, because "permission denied" alone does not say who lacks permission.
func describeUnwritable(path string, err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("Back-Orbit cannot write to %s. It runs as uid %d, so that directory, "+
			"or the volume mounted at it, has to be writable by that user", path, os.Getuid())
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("%s does not exist and neither does any directory above it, "+
			"so nothing is mounted there", path)
	case errors.Is(err, errNotADirectory):
		return fmt.Sprintf("%s exists but is not a directory", path)
	default:
		return fmt.Sprintf("%s is not usable: %v", path, err)
	}
}

func cleanAbs(path string) string {
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return absolute
}

// withinOrEqual reports whether child is root or lives underneath it. The
// separator comparison keeps "/data-backups" from counting as inside "/data".
func withinOrEqual(child, root string) bool {
	if child == root {
		return true
	}
	return strings.HasPrefix(child, root+string(os.PathSeparator))
}
