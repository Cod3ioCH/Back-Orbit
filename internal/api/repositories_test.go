package api

import (
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// signIn completes setup so the returned client is authenticated.
func signIn(t *testing.T, client *testClient) {
	t.Helper()
	client.do(http.MethodGet, "/api/v1/setup/status", nil).Body.Close()
	client.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"username": "admin",
		"password": "correct-horse-battery-staple",
	}).Body.Close()
}

func requireResticForAPI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not installed; skipping repository API test")
	}
}

func TestSecretStoreStatusEndpoint(t *testing.T) {
	client := newTestServer(t)
	signIn(t, client)

	resp := client.do(http.MethodGet, "/api/v1/secrets/status", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	status := decodeBody[map[string]any](t, resp)

	if status["initialized"] != true {
		t.Fatalf("expected initialized=true, got %v", status["initialized"])
	}
	if status["unlocked"] != true {
		t.Fatalf("expected unlocked=true, got %v", status["unlocked"])
	}
}

func TestSecretStoreLockAndUnlock(t *testing.T) {
	client := newTestServer(t)
	signIn(t, client)

	client.do(http.MethodPost, "/api/v1/secrets/lock", nil).Body.Close()

	resp := client.do(http.MethodGet, "/api/v1/secrets/status", nil)
	if decodeBody[map[string]any](t, resp)["unlocked"] != false {
		t.Fatal("expected the store to report itself locked")
	}

	// A wrong passphrase must not unlock it.
	resp = client.do(http.MethodPost, "/api/v1/secrets/unlock", map[string]string{"passphrase": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong passphrase, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = client.do(http.MethodPost, "/api/v1/secrets/unlock",
		map[string]string{"passphrase": "a-sufficiently-long-master-passphrase"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 unlocking, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = client.do(http.MethodGet, "/api/v1/secrets/status", nil)
	if decodeBody[map[string]any](t, resp)["unlocked"] != true {
		t.Fatal("expected the store to be unlocked again")
	}
}

// TestRepositoryPasswordNeverComesBack is the promise the API has to keep: a
// password goes in, and no response ever contains it again.
func TestRepositoryPasswordNeverComesBack(t *testing.T) {
	requireResticForAPI(t)
	client := newTestServer(t)
	signIn(t, client)

	const password = "unmistakable-api-repository-password-4b2c"

	resp := client.do(http.MethodPost, "/api/v1/repositories", map[string]string{
		"name":     "primary",
		"kind":     "local",
		"location": filepath.Join(t.TempDir(), "repo"),
		"password": password,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}
	created := decodeBody[map[string]any](t, resp)

	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected an id in the response")
	}

	// Every endpoint that can return repository data must be free of it.
	for _, path := range []string{
		"/api/v1/repositories",
		"/api/v1/repositories/" + id,
		"/api/v1/secrets",
		"/api/v1/audit",
	} {
		resp := client.do(http.MethodGet, path, nil)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(body), password) {
			t.Fatalf("the repository password appeared in the response from %s", path)
		}
	}
}

// TestRepositoryLifecycleOverTheAPI walks what an operator actually does:
// create a destination, discover it is empty, initialise it, confirm it is
// ready, and remove it again.
func TestRepositoryLifecycleOverTheAPI(t *testing.T) {
	requireResticForAPI(t)
	client := newTestServer(t)
	signIn(t, client)

	resp := client.do(http.MethodPost, "/api/v1/repositories", map[string]string{
		"name":     "primary",
		"kind":     "local",
		"location": filepath.Join(t.TempDir(), "repo"),
		"password": "repository-password",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	id := decodeBody[map[string]any](t, resp)["id"].(string)

	resp = client.do(http.MethodPost, "/api/v1/repositories/"+id+"/check", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check: expected 200, got %d", resp.StatusCode)
	}
	if status := decodeBody[map[string]any](t, resp)["status"]; status != "uninitialized" {
		t.Fatalf("expected a fresh destination to report uninitialized, got %v", status)
	}

	resp = client.do(http.MethodPost, "/api/v1/repositories/"+id+"/initialize", nil)
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("initialize: expected 204, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = client.do(http.MethodPost, "/api/v1/repositories/"+id+"/check", nil)
	if status := decodeBody[map[string]any](t, resp)["status"]; status != "ready" {
		t.Fatalf("expected ready after initialising, got %v", status)
	}

	resp = client.do(http.MethodDelete, "/api/v1/repositories/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = client.do(http.MethodGet, "/api/v1/repositories/"+id, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestRepositoryActionsReportALockedStore checks the one failure an operator
// can fix immediately gets its own answer rather than a generic error.
func TestRepositoryActionsReportALockedStore(t *testing.T) {
	requireResticForAPI(t)
	client := newTestServer(t)
	signIn(t, client)

	resp := client.do(http.MethodPost, "/api/v1/repositories", map[string]string{
		"name":     "primary",
		"kind":     "local",
		"location": filepath.Join(t.TempDir(), "repo"),
		"password": "repository-password",
	})
	id := decodeBody[map[string]any](t, resp)["id"].(string)

	client.do(http.MethodPost, "/api/v1/secrets/lock", nil).Body.Close()

	resp = client.do(http.MethodPost, "/api/v1/repositories/"+id+"/check", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 while the store is locked, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(strings.ToLower(string(body)), "locked") {
		t.Fatalf("the error should say the store is locked, got %s", body)
	}

	// Listing must keep working, so the operator can see what is configured.
	resp = client.do(http.MethodGet, "/api/v1/repositories", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listing must work while locked, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCreateRepositoryValidation(t *testing.T) {
	client := newTestServer(t)
	signIn(t, client)

	for name, payload := range map[string]map[string]string{
		"relative path":    {"name": "x", "kind": "local", "location": "relative", "password": "p"},
		"missing password": {"name": "x", "kind": "local", "location": "/tmp/repo"},
		"unsupported kind": {"name": "x", "kind": "s3", "location": "bucket/prefix", "password": "p"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := client.do(http.MethodPost, "/api/v1/repositories", payload)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestRepositoriesRequireAuthentication(t *testing.T) {
	client := newTestServer(t)

	resp := client.do(http.MethodGet, "/api/v1/repositories", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
