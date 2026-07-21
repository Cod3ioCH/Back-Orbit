package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

// helperMountMode mounts the source volume read-only. Staging must never be
// able to alter the data it is reading: a backup that can corrupt its own
// source is worse than no backup.
const helperMountMode = "ro"

func (c *sdkClient) CreateHelperContainer(ctx context.Context, req HelperContainerRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if req.Image == "" {
		return "", fmt.Errorf("docker: helper container needs an image")
	}
	// A source is optional. A throwaway database used to prove a dump can be
	// loaded back mounts nothing at all: its data lives in the container's own
	// layer and disappears with it, which is the point.
	if (req.Source == "") != (req.MountPath == "") {
		return "", fmt.Errorf("docker: a helper container needs both a source and a mount path, or neither")
	}
	if err := checkHelperSource(req.Source); err != nil {
		return "", err
	}

	mode := helperMountMode
	if req.Writable {
		mode = "rw"
	}

	entrypoint := []string{"/bin/true"}
	command := []string{}
	if len(req.Command) > 0 {
		// An explicit argument vector, never a shell string: the source path
		// and file names inside it come from Docker and from a user's own
		// directory, so anything assembled into a shell command would be an
		// injection waiting to happen.
		entrypoint = req.Command
	}

	body := map[string]any{
		"Image": req.Image,
		// With no Command the container is never started, so this is only what
		// would run if somebody did. Keep it inert rather than inheriting the
		// image's own entrypoint, which for the Back-Orbit image is a server.
		"Entrypoint": entrypoint,
		"Cmd":        command,
		"Labels": map[string]string{
			HelperContainerLabel: req.Purpose,
		},
		"HostConfig": map[string]any{
			"AutoRemove": false,
			// No network, in every case. A helper reads files or talks to
			// itself; anything it could reach over a network it has no
			// business reaching.
			"NetworkMode": "none",
		},
	}
	if req.Source != "" {
		body["HostConfig"].(map[string]any)["Binds"] =
			[]string{req.Source + ":" + req.MountPath + ":" + mode}
	}
	if len(req.Env) > 0 {
		body["Env"] = req.Env
	}

	if req.Server {
		// Let the image start the way it was built to. Overriding the
		// entrypoint here would start a database that never initialises.
		delete(body, "Entrypoint")
		delete(body, "Cmd")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("docker: encode helper container request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/containers/create", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("docker: build create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("docker: create helper container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("docker: create helper container returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("docker: decode create response: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("docker: daemon returned no container id")
	}
	return created.ID, nil
}

func (c *sdkClient) ContainerArchive(ctx context.Context, containerID, path string) (io.ReadCloser, error) {
	// No timeout wrapper here: this streams the whole volume, which can take
	// far longer than a control-plane call. Cancellation still works through
	// the caller's context.
	query := url.Values{}
	query.Set("path", path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/containers/"+containerID+"/archive?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("docker: build archive request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: read container archive: %w", err)
	}
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("docker: container archive returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	return resp.Body, nil
}

func (c *sdkClient) RemoveContainer(ctx context.Context, containerID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	query := url.Values{}
	query.Set("force", "true")
	// Remove anonymous volumes the helper itself may have created. The mounted
	// source volume is named, so it is never touched by this.
	query.Set("v", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/containers/"+containerID+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("docker: build remove request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: remove container: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// Already gone is the outcome we wanted, so cleanup stays idempotent.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker: remove container returned %d", resp.StatusCode)
	}
	return nil
}

func (c *sdkClient) ListHelperContainers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	query := url.Values{}
	query.Set("all", "true")
	if err := setLabelFilter(query, HelperContainerLabel); err != nil {
		return nil, err
	}

	var containers []containerSummary
	if err := c.getJSON(ctx, "/containers/json", query, &containers); err != nil {
		return nil, fmt.Errorf("docker: list helper containers: %w", err)
	}

	ids := make([]string, 0, len(containers))
	for _, container := range containers {
		ids = append(ids, container.ID)
	}
	return ids, nil
}

func (c *sdkClient) SelfImage(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	// Inside a container the hostname is the container's short id unless the
	// operator overrode it, which makes it the cheapest way to find ourselves.
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("docker: read hostname: %w", err)
	}

	var inspect struct {
		Image  string `json:"Image"`
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
	}
	if err := c.getJSON(ctx, "/containers/"+hostname+"/json", nil, &inspect); err != nil {
		return "", fmt.Errorf("docker: inspect own container (is Back-Orbit running in Docker?): %w", err)
	}

	// Prefer the human-readable tag from Config; fall back to the image id,
	// which is always usable even for an untagged build.
	if inspect.Config.Image != "" {
		return inspect.Config.Image, nil
	}
	if inspect.Image != "" {
		return inspect.Image, nil
	}
	return "", fmt.Errorf("docker: could not determine own image")
}

// dangerousBindSources are host paths a helper container must never mount.
//
// A bind source is whatever a Compose file asked for, so it can name anything
// on the host. Mounting the Docker socket would hand the helper control of the
// daemon; the kernel pseudo-filesystems contain no data worth backing up and
// reading them can block indefinitely. Refusing is better than producing a
// backup nobody wants and a risk nobody asked for.
var dangerousBindSources = []string{
	"/var/run/docker.sock",
	"/run/docker.sock",
	"/proc",
	"/sys",
	"/dev",
}

// checkHelperSource rejects a bind source that must not be mounted. A named
// volume (no leading slash) is always allowed.
func checkHelperSource(source string) error {
	if source == "" {
		return nil // nothing mounted
	}
	if !strings.HasPrefix(source, "/") {
		return nil // a named volume
	}

	clean := path.Clean(source)
	if clean == "/" {
		return fmt.Errorf("docker: refusing to mount the host root directory")
	}

	for _, forbidden := range dangerousBindSources {
		if clean == forbidden || strings.HasPrefix(clean, forbidden+"/") {
			return fmt.Errorf("docker: refusing to mount %s: %s is not something Back-Orbit will read into a backup",
				source, forbidden)
		}
	}
	return nil
}

// maxHelperOutput bounds captured helper output, so a tool that prints
// endlessly cannot exhaust memory.
const maxHelperOutput = 16 * 1024

// RunHelperContainer starts a helper and waits for it to finish.
func (c *sdkClient) RunHelperContainer(ctx context.Context, containerID string) (ContainerRunResult, error) {
	startReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/containers/"+containerID+"/start", nil)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("docker: build start request: %w", err)
	}
	startResp, err := c.http.Do(startReq)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("docker: start helper container: %w", err)
	}
	defer startResp.Body.Close()
	if startResp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(startResp.Body, 4096))
		return ContainerRunResult{}, fmt.Errorf("docker: start helper container returned %d: %s",
			startResp.StatusCode, strings.TrimSpace(string(detail)))
	}
	_, _ = io.Copy(io.Discard, startResp.Body)

	// No timeout on the wait: the work is the caller's to bound, and a hard
	// limit here would kill a legitimately slow capture of a large database.
	// Cancellation still propagates through the context.
	waitReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/containers/"+containerID+"/wait", nil)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("docker: build wait request: %w", err)
	}
	waitResp, err := c.http.Do(waitReq)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("docker: wait for helper container: %w", err)
	}
	defer waitResp.Body.Close()
	if waitResp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(waitResp.Body, 4096))
		return ContainerRunResult{}, fmt.Errorf("docker: wait returned %d: %s",
			waitResp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var waited struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := json.NewDecoder(waitResp.Body).Decode(&waited); err != nil {
		return ContainerRunResult{}, fmt.Errorf("docker: decode wait response: %w", err)
	}

	output, err := c.containerLogs(ctx, containerID)
	if err != nil {
		// The exit code is the part that decides success, so a missing
		// explanation must not turn a successful run into a failure.
		output = fmt.Sprintf("(logs unavailable: %v)", err)
	}

	return ContainerRunResult{ExitCode: waited.StatusCode, Output: output}, nil
}

// containerLogs returns a container's combined output, with Docker's stream
// framing removed.
func (c *sdkClient) containerLogs(ctx context.Context, containerID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	query := url.Values{}
	query.Set("stdout", "true")
	query.Set("stderr", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/containers/"+containerID+"/logs?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("logs returned %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxHelperOutput))
	if err != nil {
		return "", err
	}
	return string(demultiplexDockerStream(raw)), nil
}

// demultiplexDockerStream strips Docker's 8-byte stream headers.
//
// Without a TTY the daemon frames output as [stream, 0,0,0, size(4)] followed
// by the payload. Returning it raw would put control bytes into an error
// message that is meant to be read by a person.
func demultiplexDockerStream(raw []byte) []byte {
	var out []byte
	for len(raw) >= 8 {
		// A frame header always has zeroes in bytes 1..3; anything else means
		// this is plain output (a TTY-allocated container), so it is returned
		// as-is rather than mangled.
		if raw[1] != 0 || raw[2] != 0 || raw[3] != 0 {
			return raw
		}
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		out = append(out, raw[:size]...)
		raw = raw[size:]
	}
	return append(out, raw...)
}
