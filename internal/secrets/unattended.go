package secrets

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// maxKeyFileSize bounds how much is read from a key file. A passphrase is
// short; anything larger is a misconfiguration (a mounted directory, the wrong
// file) and should be reported rather than loaded into memory.
const maxKeyFileSize = 4096

// ErrNoKeyFile means unattended unlock was not configured.
var ErrNoKeyFile = errors.New("secrets: no master key file configured")

// UnlockFromFile unlocks the store using a passphrase read from path —
// typically a Docker secret mounted at /run/secrets/..., or a
// permission-restricted file on disk.
//
// This path exists because the alternative silently breaks the product. If the
// store could only be unlocked by a human typing a passphrase, then after any
// restart — an update, a host reboot, a crash at 3am — the scheduler could not
// decrypt repository passwords, every scheduled backup would fail, and the UI
// would still show the projects as protected. A backup tool that quietly stops
// backing up is worse than one that never started.
//
// A plain environment variable is deliberately not supported: environment
// variables leak through process listings, crash dumps, `docker inspect` and
// child processes far too easily to hold the key to every other credential.
func (s *Store) UnlockFromFile(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrNoKeyFile
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secrets: read master key file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("secrets: master key path %q is a directory", path)
	}
	if info.Size() > maxKeyFileSize {
		return fmt.Errorf("secrets: master key file %q is larger than %d bytes; this looks like the wrong file",
			path, maxKeyFileSize)
	}
	if err := checkKeyFilePermissions(path, info.Mode()); err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("secrets: read master key file: %w", err)
	}

	// Trailing newlines are almost always an artefact of how the file was
	// written (`echo`, an editor, a Docker secret), not part of the
	// passphrase. Stripping them avoids an unlock that fails for a reason
	// nobody can see.
	passphrase := strings.TrimRight(string(content), "\r\n")
	if passphrase == "" {
		return fmt.Errorf("secrets: master key file %q is empty", path)
	}

	return s.Unlock(ctx, passphrase)
}

// checkKeyFilePermissions refuses a key file that others can read. The file
// holds the key to every credential Back-Orbit stores, so a world-readable one
// defeats the point of encrypting them at all — and it is a mistake that is
// easy to make and impossible to notice once things "work".
func checkKeyFilePermissions(path string, mode fs.FileMode) error {
	if mode.Perm()&0o077 != 0 {
		return fmt.Errorf(
			"secrets: master key file %q is accessible to other users (mode %#o); "+
				"restrict it with chmod 600", path, mode.Perm())
	}
	return nil
}
