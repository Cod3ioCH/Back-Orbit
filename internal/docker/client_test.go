package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDockerEngine is a minimal stand-in for the Docker Engine API,
// covering just the endpoints sdkClient calls, so ListComposeProjects can be
// tested end-to-end over real HTTP without a real Docker daemon.
func fakeDockerEngine(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.43")
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		containers := []containerSummary{
			{
				ID:      "container-1",
				Names:   []string{"/myapp-web-1"},
				Image:   "nginx:latest",
				ImageID: "sha256:abc",
				Created: 1700000000,
				State:   "running",
				Status:  "Up 2 hours",
				Labels: map[string]string{
					LabelProject:     "myapp",
					LabelService:     "web",
					LabelWorkingDir:  "/srv/myapp",
					LabelConfigFiles: "/srv/myapp/compose.yml",
				},
				Mounts: []mountSummary{
					{Type: "volume", Name: "myapp_data", Destination: "/data", RW: true},
					{Type: "bind", Source: "/srv/myapp/config", Destination: "/config", RW: false},
				},
			},
			{
				ID:     "container-2",
				Names:  []string{"/other-project-app-1"},
				Image:  "alpine:latest",
				Labels: map[string]string{LabelProject: "other-project"},
			},
		}

		filters := r.URL.Query().Get("filters")
		var result []containerSummary
		for _, c := range containers {
			if matchesLabelFilter(filters, c.Labels) {
				result = append(result, c)
			}
		}

		_ = json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/volumes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(volumeListResponse{
			Volumes: []volumeSummary{
				{Name: "myapp_data", Driver: "local", Mountpoint: "/var/lib/docker/volumes/myapp_data/_data"},
				{Name: "unrelated_volume", Driver: "local", Mountpoint: "/var/lib/docker/volumes/unrelated_volume/_data"},
			},
		})
	})

	mux.HandleFunc("/networks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]networkSummary{
			{ID: "net-1", Name: "myapp_default", Driver: "bridge", Labels: map[string]string{LabelProject: "myapp"}},
		})
	})

	return httptest.NewServer(mux)
}

// matchesLabelFilter is a tiny stand-in for the Docker daemon's own label
// filter matching, just enough to exercise our client's query construction.
func matchesLabelFilter(filtersJSON string, labels map[string]string) bool {
	if filtersJSON == "" {
		return true
	}
	var parsed map[string][]string
	if err := json.Unmarshal([]byte(filtersJSON), &parsed); err != nil {
		return false
	}
	for _, labelFilter := range parsed["label"] {
		if key, value, ok := strings.Cut(labelFilter, "="); ok {
			if labels[key] != value {
				return false
			}
		} else if _, ok := labels[labelFilter]; !ok {
			return false
		}
	}
	return true
}

func TestListComposeProjects(t *testing.T) {
	server := fakeDockerEngine(t)
	defer server.Close()

	client, err := NewClient("http://" + server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	projects, err := client.ListComposeProjects(context.Background())
	if err != nil {
		t.Fatalf("ListComposeProjects: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	var myapp *ComposeProject
	for i := range projects {
		if projects[i].Name == "myapp" {
			myapp = &projects[i]
		}
	}
	if myapp == nil {
		t.Fatal("expected to find project \"myapp\"")
	}

	if len(myapp.Containers) != 1 {
		t.Fatalf("expected 1 container in myapp, got %d", len(myapp.Containers))
	}
	if myapp.Containers[0].Name != "myapp-web-1" {
		t.Fatalf("expected container name %q, got %q", "myapp-web-1", myapp.Containers[0].Name)
	}
	if myapp.Containers[0].Service != "web" {
		t.Fatalf("expected service %q, got %q", "web", myapp.Containers[0].Service)
	}

	if len(myapp.Volumes) != 1 || myapp.Volumes[0].Name != "myapp_data" {
		t.Fatalf("expected only the referenced volume myapp_data to be attached, got %+v", myapp.Volumes)
	}

	if len(myapp.Networks) != 1 || myapp.Networks[0].Name != "myapp_default" {
		t.Fatalf("expected network myapp_default to be attached, got %+v", myapp.Networks)
	}
}

func TestGetComposeProjectNotFound(t *testing.T) {
	server := fakeDockerEngine(t)
	defer server.Close()

	client, err := NewClient("http://" + server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	_, err = client.GetComposeProject(context.Background(), "does-not-exist")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestStatusReportsConnectivity(t *testing.T) {
	server := fakeDockerEngine(t)
	defer server.Close()

	client, err := NewClient("http://" + server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	status := client.Status(context.Background())
	if !status.Connected {
		t.Fatalf("expected connected status, got error: %s", status.Error)
	}
	if status.APIVersion != "1.43" {
		t.Fatalf("expected API version 1.43, got %q", status.APIVersion)
	}
}

func TestStatusReportsUnreachableDaemon(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1") // nothing listens here
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	status := client.Status(context.Background())
	if status.Connected {
		t.Fatal("expected a disconnected status for an unreachable daemon")
	}
	if status.Error == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestNewClientRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewClient("ftp://example.com")
	if err == nil {
		t.Fatal("expected an error for an unsupported host scheme")
	}
}
