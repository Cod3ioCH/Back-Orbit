package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// FakeClient is an in-memory Client implementation for tests, and for local
// development when no Docker daemon is reachable. It is exported (not
// test-only) so other packages' tests can use it without duplicating a fake.
type FakeClient struct {
	StatusResult    Status
	Projects        []ComposeProject
	ListErr         error
	GetErr          error
	ClosedCallCount int

	// ArchiveTar is handed back by ContainerArchive, letting a test drive the
	// staging code with a tar it constructed itself.
	ArchiveTar []byte
	// ArchiveErr, when set, makes ContainerArchive fail.
	ArchiveErr error
	// FakeSelfImage is returned by SelfImage.
	FakeSelfImage string

	mu sync.Mutex
	// CreatedContainers records every helper container created, and
	// RemovedContainers every one removed, so a test can assert that staging
	// cleans up after itself even when it fails.
	CreatedContainers []HelperContainerRequest
	RemovedContainers []string
	// RanContainers records helpers that were actually executed, so a test can
	// assert that a code path did — or did not — run something.
	RanContainers []string
	RunResult     ContainerRunResult
	RunErr        error
	// ExecCalls records every command run inside a container, so a test can
	// assert what a dump actually asked the database to do.
	ExecCalls      []ExecRequest
	ExecStdout     []byte
	ExecResult     ExecResult
	ExecErr        error
	EnvValues      map[string]string
	liveContainers map[string]bool
}

// NewFakeClient creates a FakeClient reporting a connected status by
// default.
func NewFakeClient(projects ...ComposeProject) *FakeClient {
	return &FakeClient{
		StatusResult: Status{Connected: true, Host: "fake://docker"},
		Projects:     projects,
	}
}

func (f *FakeClient) Status(ctx context.Context) Status {
	return f.StatusResult
}

func (f *FakeClient) ListComposeProjects(ctx context.Context) ([]ComposeProject, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Projects, nil
}

func (f *FakeClient) GetComposeProject(ctx context.Context, name string) (ComposeProject, error) {
	if f.GetErr != nil {
		return ComposeProject{}, f.GetErr
	}
	for _, p := range f.Projects {
		if p.Name == name {
			return p, nil
		}
	}
	return ComposeProject{}, ErrProjectNotFound
}

func (f *FakeClient) CreateHelperContainer(ctx context.Context, req HelperContainerRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.CreatedContainers = append(f.CreatedContainers, req)
	id := "fake-helper-" + req.Source
	if f.liveContainers == nil {
		f.liveContainers = map[string]bool{}
	}
	f.liveContainers[id] = true
	return id, nil
}

// ExecInContainer records the command and replays a scripted result.
func (f *FakeClient) ExecInContainer(ctx context.Context, containerID string, req ExecRequest) (ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// The stdin bytes are copied: a caller may reuse its buffer, and a test
	// asserting on a password would otherwise see whatever came last.
	if len(req.Stdin) > 0 {
		req.Stdin = append([]byte(nil), req.Stdin...)
	}
	f.ExecCalls = append(f.ExecCalls, req)
	if f.ExecErr != nil {
		return ExecResult{}, f.ExecErr
	}
	if req.Stdout != nil && len(f.ExecStdout) > 0 {
		if _, err := req.Stdout.Write(f.ExecStdout); err != nil {
			return ExecResult{}, err
		}
	}
	return f.ExecResult, nil
}

// ContainerEnvValue replays scripted environment values.
func (f *FakeClient) ContainerEnvValue(ctx context.Context, containerID, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.EnvValues[key], nil
}

// RunResult is what RunHelperContainer returns; RunErr overrides it.
func (f *FakeClient) RunHelperContainer(ctx context.Context, containerID string) (ContainerRunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.RanContainers = append(f.RanContainers, containerID)
	if f.RunErr != nil {
		return ContainerRunResult{}, f.RunErr
	}
	return f.RunResult, nil
}

func (f *FakeClient) ContainerArchive(ctx context.Context, containerID, path string) (io.ReadCloser, error) {
	if f.ArchiveErr != nil {
		return nil, f.ArchiveErr
	}
	return io.NopCloser(bytes.NewReader(f.ArchiveTar)), nil
}

func (f *FakeClient) RemoveContainer(ctx context.Context, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.RemovedContainers = append(f.RemovedContainers, containerID)
	delete(f.liveContainers, containerID)
	return nil
}

func (f *FakeClient) ListHelperContainers(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(f.liveContainers))
	for id := range f.liveContainers {
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *FakeClient) SelfImage(ctx context.Context) (string, error) {
	if f.FakeSelfImage == "" {
		return "", errors.New("docker: fake client has no self image configured")
	}
	return f.FakeSelfImage, nil
}

// LeakedContainers reports helper containers that were created but never
// removed — what a test asserts is empty.
func (f *FakeClient) LeakedContainers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	leaked := make([]string, 0, len(f.liveContainers))
	for id := range f.liveContainers {
		leaked = append(leaked, id)
	}
	return leaked
}

func (f *FakeClient) Close() error {
	f.ClosedCallCount++
	return nil
}
