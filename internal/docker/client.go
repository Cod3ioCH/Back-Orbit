package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// sdkClient talks to the Docker Engine API directly over HTTP, without the
// official (and dependency-heavy) Docker SDK: Back-Orbit only needs a
// handful of read-only JSON endpoints (ping, container/volume/network
// listing) in this phase, and a minimal client keeps the dependency graph
// and resulting image small. See ADR list for the rationale; if a later
// phase needs richer Docker functionality (exec, attach, image pulls for
// helper containers), this client grows the specific methods it needs
// rather than pulling in the full SDK.
type sdkClient struct {
	http    *http.Client
	baseURL string
	host    string
}

// NewClient creates a Client connected to host, which may be a Unix socket
// address ("unix:///var/run/docker.sock"), a TCP address
// ("tcp://docker-socket-proxy:2375", as used behind a Docker Socket Proxy),
// or a full HTTP(S) URL.
func NewClient(host string) (Client, error) {
	transport := &http.Transport{}
	var baseURL string

	switch {
	case strings.HasPrefix(host, "unix://"):
		socketPath := strings.TrimPrefix(host, "unix://")
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}
		baseURL = "http://docker"
	case strings.HasPrefix(host, "tcp://"):
		baseURL = "http://" + strings.TrimPrefix(host, "tcp://")
	case strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		baseURL = host
	default:
		return nil, fmt.Errorf("unsupported docker host address: %q (expected unix://, tcp://, http://, or https://)", host)
	}

	return &sdkClient{
		http:    &http.Client{Transport: transport},
		baseURL: baseURL,
		host:    host,
	}, nil
}

func (c *sdkClient) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

func (c *sdkClient) Status(ctx context.Context) Status {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", nil)
	if err != nil {
		return Status{Connected: false, Host: c.host, Error: err.Error()}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Status{Connected: false, Host: c.host, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return Status{Connected: false, Host: c.host, Error: fmt.Sprintf("docker daemon returned status %d", resp.StatusCode)}
	}

	return Status{
		Connected:     true,
		Host:          c.host,
		APIVersion:    resp.Header.Get("Api-Version"),
		ServerVersion: resp.Header.Get("Server"),
	}
}

func (c *sdkClient) ListComposeProjects(ctx context.Context) ([]ComposeProject, error) {
	return c.composeProjects(ctx, LabelProject)
}

func (c *sdkClient) GetComposeProject(ctx context.Context, name string) (ComposeProject, error) {
	projects, err := c.composeProjects(ctx, LabelProject+"="+name)
	if err != nil {
		return ComposeProject{}, err
	}
	if len(projects) == 0 {
		return ComposeProject{}, ErrProjectNotFound
	}
	return projects[0], nil
}

// composeProjects lists all containers matching labelFilter (a Docker
// label-filter value: either a bare key to test presence, or "key=value"
// for an exact match), groups them by Compose project label, and enriches
// each project with the volumes and networks it uses.
func (c *sdkClient) composeProjects(ctx context.Context, labelFilter string) ([]ComposeProject, error) {
	query := url.Values{}
	query.Set("all", "true")
	if err := setLabelFilter(query, labelFilter); err != nil {
		return nil, err
	}

	var containers []containerSummary
	if err := c.getJSON(ctx, "/containers/json", query, &containers); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	projects := make(map[string]*ComposeProject)
	var order []string

	for _, summary := range containers {
		projectName := summary.Labels[LabelProject]
		if projectName == "" {
			continue
		}

		project, ok := projects[projectName]
		if !ok {
			project = &ComposeProject{
				Name:        projectName,
				WorkingDir:  summary.Labels[LabelWorkingDir],
				ConfigFiles: splitConfigFiles(summary.Labels[LabelConfigFiles]),
				Containers:  []Container{},
				Volumes:     []Volume{},
				Networks:    []Network{},
			}
			projects[projectName] = project
			order = append(order, projectName)
		}

		project.Containers = append(project.Containers, summary.toContainer())
	}

	if len(projects) == 0 {
		return nil, nil
	}

	var volumeList volumeListResponse
	if err := c.getJSON(ctx, "/volumes", nil, &volumeList); err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	volumesByName := make(map[string]Volume, len(volumeList.Volumes))
	for _, v := range volumeList.Volumes {
		volumesByName[v.Name] = Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Labels:     v.Labels,
		}
	}

	sort.Strings(order)
	result := make([]ComposeProject, 0, len(order))

	for _, name := range order {
		project := projects[name]

		sort.Slice(project.Containers, func(i, j int) bool {
			return project.Containers[i].Name < project.Containers[j].Name
		})

		referencedVolumes := map[string]bool{}
		for _, ctr := range project.Containers {
			for _, m := range ctr.Mounts {
				if m.Type == MountTypeVolume && m.Name != "" {
					referencedVolumes[m.Name] = true
				}
			}
		}
		for volName := range referencedVolumes {
			if v, ok := volumesByName[volName]; ok {
				project.Volumes = append(project.Volumes, v)
			}
		}
		sort.Slice(project.Volumes, func(i, j int) bool {
			return project.Volumes[i].Name < project.Volumes[j].Name
		})

		netQuery := url.Values{}
		if err := setLabelFilter(netQuery, LabelProject+"="+name); err != nil {
			return nil, err
		}
		var networks []networkSummary
		if err := c.getJSON(ctx, "/networks", netQuery, &networks); err != nil {
			return nil, fmt.Errorf("list networks for project %q: %w", name, err)
		}
		for _, n := range networks {
			project.Networks = append(project.Networks, Network{
				ID:     n.ID,
				Name:   n.Name,
				Driver: n.Driver,
				Labels: n.Labels,
			})
		}
		sort.Slice(project.Networks, func(i, j int) bool {
			return project.Networks[i].Name < project.Networks[j].Name
		})

		result = append(result, *project)
	}

	return result, nil
}

func setLabelFilter(query url.Values, labelFilter string) error {
	filterJSON, err := json.Marshal(map[string][]string{"label": {labelFilter}})
	if err != nil {
		return fmt.Errorf("encode docker filter: %w", err)
	}
	query.Set("filters", string(filterJSON))
	return nil
}

// getJSON performs a GET request against the Docker Engine API and decodes
// a successful JSON response into out.
func (c *sdkClient) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker API %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response from %s: %w", path, err)
		}
	}

	return nil
}

func splitConfigFiles(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			files = append(files, p)
		}
	}
	return files
}
