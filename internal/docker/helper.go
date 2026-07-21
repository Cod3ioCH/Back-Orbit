package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// helperMountMode mounts the source volume read-only. Staging must never be
// able to alter the data it is reading: a backup that can corrupt its own
// source is worse than no backup.
const helperMountMode = "ro"

func (c *sdkClient) CreateHelperContainer(ctx context.Context, req HelperContainerRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if req.Image == "" || req.VolumeName == "" || req.MountPath == "" {
		return "", fmt.Errorf("docker: helper container needs an image, a volume and a mount path")
	}

	body := map[string]any{
		"Image": req.Image,
		// The container is never started, so this is only what would run if
		// somebody did. Keep it inert rather than inheriting the image's own
		// entrypoint, which for the Back-Orbit image would be a server.
		"Entrypoint": []string{"/bin/true"},
		"Cmd":        []string{},
		"Labels": map[string]string{
			HelperContainerLabel: req.Purpose,
		},
		"HostConfig": map[string]any{
			"Binds":       []string{req.VolumeName + ":" + req.MountPath + ":" + helperMountMode},
			"AutoRemove":  false,
			"NetworkMode": "none",
		},
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
