package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ExecRequest describes a command to run inside an existing container.
type ExecRequest struct {
	// Cmd is an argument vector, never a shell string. The values reach here
	// from Compose configuration and a user's own environment, so anything
	// assembled into a shell command would be an injection waiting to happen.
	Cmd []string

	// User overrides the user the command runs as. Empty keeps the image's own.
	User string

	// Stdout receives the command's standard output as it arrives.
	//
	// Streamed rather than returned: a database dump is exactly as large as the
	// database, and buffering it would make memory use a function of the thing
	// being backed up.
	Stdout io.Writer
}

// ExecResult reports how the command finished.
type ExecResult struct {
	ExitCode int
	// Stderr is bounded. It is the only explanation available when a dump
	// fails, but it comes from a foreign process and is untrusted text.
	Stderr string
}

// maxExecStderr bounds captured error output.
const maxExecStderr = 16 * 1024

// ExecInContainer runs a command inside a running container and streams its
// output.
//
// Used for database dumps, where the container's own tooling is the only
// correct choice: pg_dump refuses to dump a server newer than itself, so a
// helper container carrying some other version fails precisely when a backup
// is needed. The binaries beside the running server always match it.
func (c *sdkClient) ExecInContainer(ctx context.Context, containerID string, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, fmt.Errorf("docker: exec needs a command")
	}
	if req.Stdout == nil {
		return ExecResult{}, fmt.Errorf("docker: exec needs somewhere to put its output")
	}

	create := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"AttachStdin":  false,
		"Tty":          false,
		"Cmd":          req.Cmd,
	}
	if req.User != "" {
		create["User"] = req.User
	}

	execID, err := c.createExec(ctx, containerID, create)
	if err != nil {
		return ExecResult{}, err
	}

	stderr, err := c.startExec(ctx, execID, req.Stdout)
	if err != nil {
		return ExecResult{}, err
	}

	code, err := c.execExitCode(ctx, execID)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{ExitCode: code, Stderr: stderr}, nil
}

func (c *sdkClient) createExec(ctx context.Context, containerID string, body map[string]any) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("docker: encode exec request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/containers/"+containerID+"/exec", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("docker: build exec request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("docker: create exec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("docker: create exec returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("docker: decode exec response: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("docker: daemon returned no exec id")
	}
	return created.ID, nil
}

// startExec runs the command and demultiplexes its output stream.
func (c *sdkClient) startExec(ctx context.Context, execID string, stdout io.Writer) (string, error) {
	payload, err := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return "", err
	}

	// No timeout: a dump takes as long as the database is large, and the
	// caller's context is what bounds it.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/exec/"+execID+"/start", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("docker: build exec start: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("docker: start exec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("docker: start exec returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var stderr bytes.Buffer
	if err := demultiplex(resp.Body, stdout, &stderr); err != nil {
		return stderr.String(), fmt.Errorf("docker: read exec output: %w", err)
	}
	return stderr.String(), nil
}

func (c *sdkClient) execExitCode(ctx context.Context, execID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var inspect struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	if err := c.getJSON(ctx, "/exec/"+execID+"/json", nil, &inspect); err != nil {
		return 0, fmt.Errorf("docker: inspect exec: %w", err)
	}
	return inspect.ExitCode, nil
}

// demultiplex splits Docker's framed output into stdout and stderr.
//
// Without a TTY the daemon frames each chunk as [stream, 0, 0, 0, size(4)].
// Reading it as one stream would interleave error text into a database dump,
// which is a corrupt dump that still looks like SQL.
func demultiplex(r io.Reader, stdout, stderr io.Writer) error {
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}

		size := int64(binary.BigEndian.Uint32(header[4:8]))
		if size == 0 {
			continue
		}

		target := stdout
		if header[0] == 2 {
			// Bounded, so a process writing errors forever cannot exhaust
			// memory while the dump itself streams to disk unaffected.
			target = &boundedWriter{w: stderr, remaining: maxExecStderr}
		}
		if _, err := io.CopyN(target, r, size); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// boundedWriter discards everything past a limit.
type boundedWriter struct {
	w         io.Writer
	remaining int
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.remaining <= 0 {
		return len(p), nil
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.w.Write(p)
	b.remaining -= n
	return len(p), err
}

// ContainerEnvValue reads one environment variable from a running container.
//
// Deliberately one key at a time rather than the whole environment. A
// container's environment is where database passwords live, and a function
// that hands back all of it invites a caller to log or store the lot. The only
// value Back-Orbit reads this way is the database user name a dump must
// connect as — the password is never needed, because the dump runs inside the
// container over its local socket.
//
// An absent variable is not an error: callers fall back to the image's own
// default, which is what an unconfigured container uses.
func (c *sdkClient) ContainerEnvValue(ctx context.Context, containerID, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var inspect struct {
		Config struct {
			Env []string `json:"Env"`
		} `json:"Config"`
	}
	if err := c.getJSON(ctx, "/containers/"+containerID+"/json", nil, &inspect); err != nil {
		return "", fmt.Errorf("docker: inspect container: %w", err)
	}

	prefix := key + "="
	for _, entry := range inspect.Config.Env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), nil
		}
	}
	return "", nil
}
