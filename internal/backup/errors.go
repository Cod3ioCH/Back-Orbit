package backup

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKind classifies a failure so callers can react without parsing
// messages. The job system uses this to decide whether a failure is worth
// retrying (a locked repository or a network blip) or is pointless to retry
// (a wrong password, a missing repository).
type ErrorKind string

const (
	// ErrKindUnknown is an unclassified failure.
	ErrKindUnknown ErrorKind = "unknown"
	// ErrKindRepositoryNotFound means the repository does not exist yet.
	ErrKindRepositoryNotFound ErrorKind = "repository_not_found"
	// ErrKindRepositoryExists means initialisation found an existing repository.
	ErrKindRepositoryExists ErrorKind = "repository_exists"
	// ErrKindWrongPassword means the repository password did not decrypt it.
	ErrKindWrongPassword ErrorKind = "wrong_password"
	// ErrKindRepositoryLocked means another process holds the repository lock.
	ErrKindRepositoryLocked ErrorKind = "repository_locked"
	// ErrKindNetwork covers unreachable hosts, DNS and connection failures.
	ErrKindNetwork ErrorKind = "network"
	// ErrKindPermission covers permission-denied on repository or source data.
	ErrKindPermission ErrorKind = "permission"
	// ErrKindNoSpace means the destination ran out of space.
	ErrKindNoSpace ErrorKind = "no_space"
	// ErrKindCancelled means the operation was cancelled or interrupted.
	ErrKindCancelled ErrorKind = "cancelled"
	// ErrKindIncompleteRead means some source files could not be read. The
	// snapshot was still created, so this is reported as a warning-level
	// failure rather than a lost backup.
	ErrKindIncompleteRead ErrorKind = "incomplete_read"
	// ErrKindEngineMissing means the restic binary is not available.
	ErrKindEngineMissing ErrorKind = "engine_missing"
	// ErrKindEngineUnsupported means the installed restic is too old.
	ErrKindEngineUnsupported ErrorKind = "engine_unsupported"
)

// Retryable reports whether retrying the same operation could plausibly
// succeed without a human changing something first.
func (k ErrorKind) Retryable() bool {
	switch k {
	case ErrKindRepositoryLocked, ErrKindNetwork, ErrKindNoSpace:
		return true
	default:
		return false
	}
}

// EngineError is a classified failure from the backup engine. It deliberately
// carries only already-redacted output: constructing it is the single place
// where tool output crosses into Back-Orbit's own error handling, so it must
// never smuggle a repository password into a log line.
type EngineError struct {
	Kind     ErrorKind
	Op       string
	ExitCode int
	// Output is the redacted tail of the tool's stderr/stdout.
	Output string
	Err    error
}

func (e *EngineError) Error() string {
	var b strings.Builder
	b.WriteString("restic ")
	b.WriteString(e.Op)
	b.WriteString(": ")
	b.WriteString(string(e.Kind))
	if e.ExitCode != 0 {
		fmt.Fprintf(&b, " (exit %d)", e.ExitCode)
	}
	if e.Output != "" {
		b.WriteString(": ")
		b.WriteString(e.Output)
	}
	return b.String()
}

func (e *EngineError) Unwrap() error { return e.Err }

// KindOf returns the classification of err, or ErrKindUnknown if err did not
// come from the engine.
func KindOf(err error) ErrorKind {
	var engineErr *EngineError
	if errors.As(err, &engineErr) {
		return engineErr.Kind
	}
	return ErrKindUnknown
}

// resticExitCodes documents the exit codes restic assigns specific meanings.
// Anything else falls back to inspecting the message.
const (
	exitCodeIncompleteRead   = 3
	exitCodeRepositoryAbsent = 10
	exitCodeLockFailed       = 11
	exitCodeWrongPassword    = 12
	exitCodeInterrupted      = 130
)

// classify turns an exit code plus tool output into an ErrorKind. Exit codes
// are checked first because they are unambiguous; message matching is only a
// fallback for the generic exit code 1, and for restic versions older than
// the ones that introduced the specific codes.
func classify(exitCode int, output string) ErrorKind {
	switch exitCode {
	case exitCodeIncompleteRead:
		return ErrKindIncompleteRead
	case exitCodeRepositoryAbsent:
		return ErrKindRepositoryNotFound
	case exitCodeLockFailed:
		return ErrKindRepositoryLocked
	case exitCodeWrongPassword:
		return ErrKindWrongPassword
	case exitCodeInterrupted:
		return ErrKindCancelled
	}

	lower := strings.ToLower(output)
	switch {
	case containsAny(lower, "wrong password", "invalid password", "bad password"):
		return ErrKindWrongPassword
	case containsAny(lower, "config file already exists", "repository master key and config already initialized", "already initialized"):
		return ErrKindRepositoryExists
	case containsAny(lower, "unable to open config file", "repository does not exist", "no such file or directory) is not a valid repository"):
		return ErrKindRepositoryNotFound
	case containsAny(lower, "unable to create lock", "repository is already locked"):
		return ErrKindRepositoryLocked
	case containsAny(lower, "no space left", "not enough space", "quota exceeded"):
		return ErrKindNoSpace
	case containsAny(lower, "permission denied", "operation not permitted"):
		return ErrKindPermission
	case containsAny(lower, "connection refused", "no such host", "network is unreachable",
		"i/o timeout", "connection reset", "dial tcp", "tls handshake"):
		return ErrKindNetwork
	case containsAny(lower, "context canceled", "interrupt"):
		return ErrKindCancelled
	}

	return ErrKindUnknown
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
