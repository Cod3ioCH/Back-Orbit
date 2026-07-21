package backup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// minimumResticVersion is the oldest restic Back-Orbit will run against.
// 0.17 introduced the distinct exit codes this package classifies failures
// with; on older versions those failures would be indistinguishable from a
// generic error.
var minimumResticVersion = version{0, 17, 0}

// maxCapturedOutput bounds how much tool output is kept for error messages,
// so a pathological failure cannot blow up memory or a log line.
const maxCapturedOutput = 8 * 1024

// ResticEngine runs the restic binary. It holds no mutable state, so a single
// instance is safe for concurrent use.
type ResticEngine struct {
	// binary is the restic executable; defaults to "restic" resolved on PATH.
	binary string

	// checkVersionOnce ensures the (relatively slow) version probe runs once
	// per engine rather than on every operation.
	checkVersionOnce sync.Once
	versionErr       error
}

// NewResticEngine creates an engine using the given binary. An empty binary
// resolves "restic" from PATH.
func NewResticEngine(binary string) *ResticEngine {
	if binary == "" {
		binary = "restic"
	}
	return &ResticEngine{binary: binary}
}

var _ BackupEngine = (*ResticEngine)(nil)

// EnsureAvailable verifies the restic binary exists and is new enough. It is
// called before every operation, but the underlying probe runs only once.
func (e *ResticEngine) EnsureAvailable(ctx context.Context) error {
	e.checkVersionOnce.Do(func() {
		e.versionErr = e.probeVersion(ctx)
	})
	return e.versionErr
}

func (e *ResticEngine) probeVersion(ctx context.Context) error {
	if _, err := exec.LookPath(e.binary); err != nil {
		return &EngineError{
			Kind: ErrKindEngineMissing,
			Op:   "version",
			Err:  fmt.Errorf("restic binary %q not found: %w", e.binary, err),
		}
	}

	cmd := exec.CommandContext(ctx, e.binary, "version")
	cmd.Env = baseEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return &EngineError{Kind: ErrKindEngineMissing, Op: "version", Err: err}
	}

	got, parseErr := parseResticVersion(string(out))
	if parseErr != nil {
		// An unparseable version string is not worth refusing to run over —
		// a custom build may format it differently — but it is worth noting.
		return nil
	}
	if got.olderThan(minimumResticVersion) {
		return &EngineError{
			Kind: ErrKindEngineUnsupported,
			Op:   "version",
			Err: fmt.Errorf("restic %s is too old, need at least %s",
				got, minimumResticVersion),
		}
	}
	return nil
}

func (e *ResticEngine) InitRepository(ctx context.Context, config RepositoryConfig) error {
	_, err := e.run(ctx, config, runOptions{op: "init", args: []string{"init"}})
	return err
}

func (e *ResticEngine) ListSnapshots(ctx context.Context, repository RepositoryConfig) ([]Snapshot, error) {
	result, err := e.run(ctx, repository, runOptions{
		op:   "snapshots",
		args: []string{"snapshots", "--json"},
	})
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID       string    `json:"id"`
		ShortID  string    `json:"short_id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
		Tags     []string  `json:"tags"`
		Paths    []string  `json:"paths"`
	}
	if err := json.Unmarshal(result.stdout.Bytes(), &raw); err != nil {
		return nil, &EngineError{Kind: ErrKindUnknown, Op: "snapshots", Err: fmt.Errorf("decode snapshot list: %w", err)}
	}

	snapshots := make([]Snapshot, 0, len(raw))
	for _, r := range raw {
		snapshots = append(snapshots, Snapshot{
			ID:      r.ID,
			ShortID: r.ShortID,
			Time:    r.Time,
			Host:    r.Hostname,
			Tags:    r.Tags,
			Paths:   r.Paths,
		})
	}
	return snapshots, nil
}

func (e *ResticEngine) CreateSnapshot(ctx context.Context, request SnapshotRequest) (*SnapshotResult, error) {
	if len(request.Paths) == 0 {
		return nil, &EngineError{Kind: ErrKindUnknown, Op: "backup", Err: errors.New("no paths to back up")}
	}

	args := []string{"backup", "--json"}
	for _, tag := range request.Tags {
		args = append(args, "--tag", tag)
	}
	for _, exclude := range request.Excludes {
		args = append(args, "--exclude", exclude)
	}
	if request.Host != "" {
		args = append(args, "--host", request.Host)
	}
	// "--" stops flag parsing so a path that begins with a dash can never be
	// interpreted as an option.
	args = append(args, "--")
	args = append(args, request.Paths...)

	var (
		summary  backupSummary
		warnings []string
	)

	started := time.Now()
	result, err := e.run(ctx, request.Repository, runOptions{
		op:   "backup",
		args: args,
		onStdoutLine: func(line []byte) {
			msg := decodeMessageType(line)
			switch msg {
			case "status":
				if request.OnProgress != nil {
					if p, ok := decodeProgress(line); ok {
						request.OnProgress(p)
					}
				}
			case "summary":
				_ = json.Unmarshal(line, &summary)
			case "error":
				if w := decodeErrorMessage(line); w != "" {
					warnings = append(warnings, w)
				}
			}
		},
	})

	// Exit code 3 means restic could not read some source files but still
	// wrote a snapshot. Losing that snapshot by treating it as a hard failure
	// would be worse than reporting it with warnings.
	if err != nil && KindOf(err) != ErrKindIncompleteRead {
		return nil, err
	}
	if err != nil {
		warnings = append(warnings, "some files could not be read and were skipped")
	}

	if summary.SnapshotID == "" {
		return nil, &EngineError{
			Kind:   ErrKindUnknown,
			Op:     "backup",
			Output: redact(result.tail(), request.Repository),
			Err:    errors.New("restic reported no snapshot id"),
		}
	}

	return &SnapshotResult{
		SnapshotID:      summary.SnapshotID,
		FilesNew:        summary.FilesNew,
		FilesChanged:    summary.FilesChanged,
		FilesUnmodified: summary.FilesUnmodified,
		DataAdded:       summary.DataAdded,
		TotalBytes:      summary.TotalBytesProcessed,
		TotalFiles:      summary.TotalFilesProcessed,
		Duration:        time.Since(started),
		Warnings:        warnings,
	}, nil
}

func (e *ResticEngine) RestoreSnapshot(ctx context.Context, request RestoreRequest) (*RestoreResult, error) {
	if request.TargetPath == "" {
		return nil, &EngineError{Kind: ErrKindUnknown, Op: "restore", Err: errors.New("no restore target path")}
	}
	snapshotID := request.SnapshotID
	if snapshotID == "" {
		snapshotID = "latest"
	}

	args := []string{"restore", "--json", "--target", request.TargetPath}
	for _, include := range request.Include {
		args = append(args, "--include", include)
	}
	args = append(args, "--", snapshotID)

	var summary restoreSummary
	started := time.Now()
	_, err := e.run(ctx, request.Repository, runOptions{
		op:   "restore",
		args: args,
		onStdoutLine: func(line []byte) {
			if decodeMessageType(line) == "summary" {
				_ = json.Unmarshal(line, &summary)
			}
		},
	})
	if err != nil {
		return nil, err
	}

	return &RestoreResult{
		FilesRestored: summary.FilesRestored,
		BytesRestored: summary.BytesRestored,
		Duration:      time.Since(started),
	}, nil
}

func (e *ResticEngine) VerifyRepository(ctx context.Context, repository RepositoryConfig) (*VerificationResult, error) {
	started := time.Now()
	result, err := e.run(ctx, repository, runOptions{op: "check", args: []string{"check"}})
	if err != nil {
		// A check that finds damage exits non-zero. That is a verification
		// result, not an engine malfunction, so report it as one — except
		// for problems that stopped the check from running at all.
		kind := KindOf(err)
		if kind == ErrKindWrongPassword || kind == ErrKindRepositoryNotFound ||
			kind == ErrKindEngineMissing || kind == ErrKindCancelled {
			return nil, err
		}
		return &VerificationResult{
			OK:       false,
			Duration: time.Since(started),
			Errors:   []string{redact(result.tail(), repository)},
		}, nil
	}

	return &VerificationResult{OK: true, Duration: time.Since(started)}, nil
}

func (e *ResticEngine) ApplyRetention(ctx context.Context, repository RepositoryConfig, policy RetentionPolicy) error {
	if policy.IsZero() {
		// A policy with no rules would match nothing to keep. Refusing is the
		// safe reading of an empty policy: it is far more likely to be a
		// configuration mistake than a request to delete every snapshot.
		return &EngineError{
			Kind: ErrKindUnknown,
			Op:   "forget",
			Err:  errors.New("refusing to apply a retention policy with no keep rules"),
		}
	}

	args := []string{"forget"}

	// Scope first: a plan may only prune its own snapshots.
	for _, tag := range policy.OnlyTags {
		args = append(args, "--tag", tag)
	}

	// Always state the grouping explicitly — see RetentionPolicy.GroupBy for
	// why relying on restic's default would quietly break retention.
	args = append(args, "--group-by", policy.GroupBy)

	appendKeep := func(flag string, value int) {
		if value > 0 {
			args = append(args, flag, fmt.Sprintf("%d", value))
		}
	}
	appendKeep("--keep-last", policy.KeepLast)
	appendKeep("--keep-hourly", policy.KeepHourly)
	appendKeep("--keep-daily", policy.KeepDaily)
	appendKeep("--keep-weekly", policy.KeepWeekly)
	appendKeep("--keep-monthly", policy.KeepMonthly)
	appendKeep("--keep-yearly", policy.KeepYearly)
	if policy.KeepWithin > 0 {
		args = append(args, "--keep-within", formatDuration(policy.KeepWithin))
	}
	for _, tag := range policy.KeepTags {
		args = append(args, "--keep-tag", tag)
	}

	_, err := e.run(ctx, repository, runOptions{op: "forget", args: args})
	return err
}

func (e *ResticEngine) PruneRepository(ctx context.Context, repository RepositoryConfig) error {
	_, err := e.run(ctx, repository, runOptions{op: "prune", args: []string{"prune"}})
	return err
}

// runOptions describes one restic invocation.
type runOptions struct {
	op   string
	args []string
	// onStdoutLine, when set, receives each stdout line as it arrives. Used
	// for restic's --json streaming output.
	onStdoutLine func(line []byte)
}

type runResult struct {
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	exitCode int
}

// tail returns the most useful part of the captured output for an error
// message, preferring stderr.
func (r runResult) tail() string {
	if r.stderr != nil && r.stderr.Len() > 0 {
		return strings.TrimSpace(r.stderr.String())
	}
	if r.stdout != nil {
		return strings.TrimSpace(r.stdout.String())
	}
	return ""
}

// run executes restic with the given arguments.
//
// Security properties this function is responsible for:
//   - The command is built as an argument slice and executed with
//     exec.CommandContext. There is no shell, so no metacharacter in a path
//     or tag can ever be interpreted.
//   - Secrets travel in the environment, never in argv, because argv is
//     readable by any local user through `ps`.
//   - The environment is constructed explicitly rather than inherited, so an
//     unrelated variable in Back-Orbit's own environment cannot alter restic's
//     behaviour.
//   - Captured output is bounded and redacted before it reaches an error.
func (e *ResticEngine) run(ctx context.Context, config RepositoryConfig, opts runOptions) (runResult, error) {
	if err := e.EnsureAvailable(ctx); err != nil {
		return runResult{}, err
	}

	repoURL, err := repositoryURL(config)
	if err != nil {
		return runResult{}, &EngineError{Kind: ErrKindUnknown, Op: opts.op, Err: err}
	}

	args := append([]string{"--repo", repoURL}, opts.args...)
	cmd := exec.CommandContext(ctx, e.binary, args...)
	cmd.Env = environmentFor(config)

	stdout := &boundedBuffer{limit: maxCapturedOutput}
	stderr := &boundedBuffer{limit: maxCapturedOutput}

	var stdoutPipe io.ReadCloser
	if opts.onStdoutLine != nil {
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return runResult{}, &EngineError{Kind: ErrKindUnknown, Op: opts.op, Err: err}
		}
	} else {
		cmd.Stdout = stdout
	}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		// A context that is already cancelled makes Start fail. That is the
		// job system aborting the run, not a broken repository, and must be
		// reported as a cancellation so a deliberately stopped job is not
		// recorded as an unexplained failure.
		kind := ErrKindUnknown
		if ctx.Err() != nil {
			kind = ErrKindCancelled
		}
		return runResult{}, &EngineError{Kind: kind, Op: opts.op, Err: err}
	}

	var streamWG sync.WaitGroup
	if stdoutPipe != nil {
		streamWG.Add(1)
		go func() {
			defer streamWG.Done()
			scanner := bufio.NewScanner(stdoutPipe)
			// restic status lines carry the list of files in flight and can
			// exceed the default 64 KiB scanner limit on wide trees.
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Bytes()
				stdout.Write(line)
				stdout.Write([]byte{'\n'})
				opts.onStdoutLine(line)
			}
		}()
	}

	waitErr := cmd.Wait()
	streamWG.Wait()

	result := runResult{stdout: &stdout.buf, stderr: &stderr.buf}

	if waitErr == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		result.exitCode = exitErr.ExitCode()
	}

	// A cancelled context is the job system aborting the run, not a repository
	// problem — report it as such regardless of how the process died.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, &EngineError{
			Kind:     ErrKindCancelled,
			Op:       opts.op,
			ExitCode: result.exitCode,
			Err:      ctxErr,
		}
	}

	output := redact(result.tail(), config)
	return result, &EngineError{
		Kind:     classify(result.exitCode, output),
		Op:       opts.op,
		ExitCode: result.exitCode,
		Output:   output,
		Err:      waitErr,
	}
}

// repositoryURL builds the restic repository string for a configuration.
func repositoryURL(config RepositoryConfig) (string, error) {
	location := strings.TrimSpace(config.Location)
	if location == "" {
		return "", errors.New("repository location is empty")
	}

	switch config.Kind {
	case RepositoryLocal:
		return location, nil
	case RepositorySFTP:
		return "sftp:" + location, nil
	case RepositoryS3:
		return "s3:" + location, nil
	case "":
		return "", errors.New("repository kind is empty")
	default:
		return "", fmt.Errorf("unsupported repository kind %q", config.Kind)
	}
}

// baseEnvironment is the minimal environment restic runs with. Building it
// explicitly, instead of inheriting os.Environ(), keeps unrelated variables
// in Back-Orbit's own environment from changing how restic behaves.
func baseEnvironment() []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		env = append(env, "TMPDIR="+tmp)
	}
	return env
}

// environmentFor adds the secrets restic needs for this repository. These are
// the only place credentials appear, and redactEnvironment knows how to strip
// them again for logging.
func environmentFor(config RepositoryConfig) []string {
	env := baseEnvironment()
	env = append(env, "RESTIC_PASSWORD="+config.Password)
	if config.Kind == RepositoryS3 {
		if config.S3AccessKeyID != "" {
			env = append(env, "AWS_ACCESS_KEY_ID="+config.S3AccessKeyID)
		}
		if config.S3SecretAccessKey != "" {
			env = append(env, "AWS_SECRET_ACCESS_KEY="+config.S3SecretAccessKey)
		}
	}
	return env
}

// secretEnvPrefixes are the environment variables whose values must never be
// logged.
var secretEnvPrefixes = []string{
	"RESTIC_PASSWORD",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_ACCESS_KEY_ID",
}

// RedactEnvironment returns env with the value of every known secret variable
// replaced. Use it anywhere an environment is logged or attached to an error.
func RedactEnvironment(env []string) []string {
	redacted := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if found && isSecretEnv(name) {
			redacted = append(redacted, name+"=[redacted]")
			continue
		}
		redacted = append(redacted, entry)
	}
	return redacted
}

func isSecretEnv(name string) bool {
	for _, prefix := range secretEnvPrefixes {
		if name == prefix {
			return true
		}
	}
	return false
}

// redact removes any secret value that leaked into tool output. restic does
// not normally echo passwords, but output is untrusted here: it may include
// URLs or messages built from the configuration, and a credential reaching a
// log or an error message would outlive the process.
func redact(output string, config RepositoryConfig) string {
	if output == "" {
		return ""
	}
	for _, secret := range []string{config.Password, config.S3SecretAccessKey, config.S3AccessKeyID} {
		// Very short secrets are skipped: replacing a one- or two-character
		// string would mangle unrelated output without protecting anything
		// meaningful.
		if len(secret) >= 4 {
			output = strings.ReplaceAll(output, secret, "[redacted]")
		}
	}
	return output
}

// boundedBuffer captures at most limit bytes, so unbounded tool output cannot
// exhaust memory. Writes past the limit are discarded but still reported as
// accepted, since the caller is a process pipe that must not be blocked.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
	mu    sync.Mutex
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			b.buf.Write(p[:remaining])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

// backupSummary and restoreSummary mirror restic's --json summary messages.
type backupSummary struct {
	SnapshotID          string  `json:"snapshot_id"`
	FilesNew            int64   `json:"files_new"`
	FilesChanged        int64   `json:"files_changed"`
	FilesUnmodified     int64   `json:"files_unmodified"`
	DataAdded           int64   `json:"data_added"`
	TotalFilesProcessed int64   `json:"total_files_processed"`
	TotalBytesProcessed int64   `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
}

type restoreSummary struct {
	FilesRestored int64 `json:"files_restored"`
	BytesRestored int64 `json:"total_bytes_restored"`
}

func decodeMessageType(line []byte) string {
	var envelope struct {
		MessageType string `json:"message_type"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return ""
	}
	return envelope.MessageType
}

func decodeProgress(line []byte) (Progress, bool) {
	var status struct {
		PercentDone  float64  `json:"percent_done"`
		FilesDone    int64    `json:"files_done"`
		TotalFiles   int64    `json:"total_files"`
		BytesDone    int64    `json:"bytes_done"`
		TotalBytes   int64    `json:"total_bytes"`
		CurrentFiles []string `json:"current_files"`
	}
	if err := json.Unmarshal(line, &status); err != nil {
		return Progress{}, false
	}
	return Progress{
		PercentDone:  status.PercentDone,
		FilesDone:    status.FilesDone,
		TotalFiles:   status.TotalFiles,
		BytesDone:    status.BytesDone,
		TotalBytes:   status.TotalBytes,
		CurrentFiles: status.CurrentFiles,
	}, true
}

func decodeErrorMessage(line []byte) string {
	var msg struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		During string `json:"during"`
		Item   string `json:"item"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}
	if msg.Error.Message == "" {
		return ""
	}
	if msg.Item != "" {
		return fmt.Sprintf("%s: %s", msg.Item, msg.Error.Message)
	}
	return msg.Error.Message
}

// formatDuration renders a duration the way restic's --keep-within expects
// (e.g. "30d", "12h").
func formatDuration(d time.Duration) string {
	if days := int(d.Hours()) / 24; days > 0 && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
