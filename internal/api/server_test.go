package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/auth"
	"github.com/Cod3ioCH/Back-Orbit/internal/config"
	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/secrets"
)

// testClient wraps httptest's server with a cookie jar (so session and CSRF
// cookies persist across requests, like a real browser) and a helper that
// attaches the CSRF header automatically.
type testClient struct {
	t       *testing.T
	baseURL string
	http    *http.Client
	server  *Server
}

func newTestServer(t *testing.T) *testClient {
	t.Helper()

	db := dbtest.Open(t)
	cfg := config.Config{
		SessionCookieName: "backorbit_session",
		SessionTTL:        time.Hour,
	}
	fake := docker.NewFakeClient()

	// Initialised and unlocked, so tests exercise the same state a running
	// instance has after an unattended unlock.
	secretStore := secrets.NewStore(db)
	if err := secretStore.Initialize(context.Background(), "a-sufficiently-long-master-passphrase"); err != nil {
		t.Fatalf("initialise secret store: %v", err)
	}

	server := NewServer(cfg, db, fake, secretStore, nil)
	ts := httptest.NewServer(server.Router())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	return &testClient{t: t, baseURL: ts.URL, http: &http.Client{Jar: jar}, server: server}
}

func (c *testClient) csrfToken() string {
	u, _ := http.NewRequest(http.MethodGet, c.baseURL, nil)
	for _, cookie := range c.http.Jar.Cookies(u.URL) {
		if cookie.Name == auth.CSRFCookieName {
			return cookie.Value
		}
	}
	return ""
}

func (c *testClient) do(method, path string, body any) *http.Response {
	c.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set(auth.CSRFHeaderName, c.csrfToken())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return v
}

func TestFullSetupLoginProjectFlow(t *testing.T) {
	client := newTestServer(t)

	// Prime the CSRF cookie.
	resp := client.do(http.MethodGet, "/api/v1/setup/status", nil)
	status := decodeBody[map[string]bool](t, resp)
	if status["setupComplete"] {
		t.Fatal("expected setup to be incomplete initially")
	}

	// Create the admin account.
	resp = client.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"username": "admin",
		"password": "correct-horse-battery-staple",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating admin, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// A second setup attempt must be rejected.
	resp = client.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"username": "second",
		"password": "another-long-password",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on second setup attempt, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The session cookie from setup should already authenticate us.
	resp = client.do(http.MethodGet, "/api/v1/auth/session", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for session check, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Projects list should be reachable and empty.
	resp = client.do(http.MethodGet, "/api/v1/projects", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing projects, got %d", resp.StatusCode)
	}
	projects := decodeBody[[]map[string]any](t, resp)
	if len(projects) != 0 {
		t.Fatalf("expected no projects yet, got %+v", projects)
	}

	// Register a project.
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yml"), []byte("services:\n  db:\n    image: postgres:17\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp = client.do(http.MethodPost, "/api/v1/projects", map[string]string{
		"name": "myproject",
		"path": projectDir,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 registering project, got %d", resp.StatusCode)
	}
	registered := decodeBody[map[string]any](t, resp)
	projectID, _ := registered["id"].(string)
	resp = client.do(http.MethodPost, "/api/v1/projects/"+projectID+"/analyze", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 analyzing project, got %d", resp.StatusCode)
	}
	blueprint := decodeBody[map[string]any](t, resp)
	if findings, ok := blueprint["findings"].([]any); !ok || len(findings) == 0 {
		t.Fatalf("expected analyzer findings, got %#v", blueprint)
	}
	resp = client.do(http.MethodPost, "/api/v1/projects/"+projectID+"/blueprint/confirm", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 confirming blueprint, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Removing a project only removes its registration and is authenticated,
	// CSRF-protected, and auditable like every destructive API operation.
	resp = client.do(http.MethodDelete, "/api/v1/projects/"+projectID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 removing project, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = client.do(http.MethodGet, "/api/v1/projects/"+projectID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected removed project to return 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Logout.
	resp = client.do(http.MethodPost, "/api/v1/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Session should now be invalid.
	resp = client.do(http.MethodGet, "/api/v1/auth/session", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	client := newTestServer(t)
	client.do(http.MethodGet, "/api/v1/setup/status", nil).Body.Close()
	client.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"username": "admin",
		"password": "correct-horse-battery-staple",
	}).Body.Close()
	client.do(http.MethodPost, "/api/v1/auth/logout", nil).Body.Close()

	resp := client.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "wrong-password",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestActivityStreamStopsOnShutdown guards the graceful-shutdown path: the
// SSE handler never completes on its own, so without an explicit shutdown
// signal http.Server.Shutdown would block until its timeout (and then report
// a deadline error) whenever a browser had the activity feed open.
func TestActivityStreamStopsOnShutdown(t *testing.T) {
	client := newTestServer(t)
	client.do(http.MethodGet, "/api/v1/setup/status", nil).Body.Close()
	client.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"username": "admin",
		"password": "correct-horse-battery-staple",
	}).Body.Close()

	resp := client.do(http.MethodGet, "/api/v1/activity/stream", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 opening the activity stream, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	streamEnded := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, resp.Body)
		streamEnded <- err
	}()

	// The stream must still be open before shutdown is signalled.
	select {
	case <-streamEnded:
		t.Fatal("activity stream closed before shutdown was signalled")
	case <-time.After(100 * time.Millisecond):
	}

	client.server.Shutdown()

	select {
	case <-streamEnded:
		// Handler returned promptly, so http.Server.Shutdown would not stall.
	case <-time.After(5 * time.Second):
		t.Fatal("activity stream did not close after shutdown was signalled")
	}

	// Shutdown must be safe to call more than once (http.Server may invoke
	// the registered hook alongside an explicit call).
	client.server.Shutdown()
}

func TestProjectsRequiresAuthentication(t *testing.T) {
	client := newTestServer(t)

	resp := client.do(http.MethodGet, "/api/v1/projects", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestStateChangingRequestWithoutCSRFTokenIsRejected(t *testing.T) {
	client := newTestServer(t)
	client.do(http.MethodGet, "/api/v1/setup/status", nil).Body.Close()

	req, err := http.NewRequest(http.MethodPost, client.baseURL+"/api/v1/setup/admin", bytes.NewReader([]byte(`{"username":"admin","password":"correct-horse-battery-staple"}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Deliberately not attaching the CSRF header.
	resp, err := client.http.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without a CSRF token, got %d", resp.StatusCode)
	}
}

func TestHealthzDoesNotRequireAuth(t *testing.T) {
	client := newTestServer(t)

	resp := client.do(http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /healthz, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
