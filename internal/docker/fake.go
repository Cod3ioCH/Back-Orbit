package docker

import "context"

// FakeClient is an in-memory Client implementation for tests, and for local
// development when no Docker daemon is reachable. It is exported (not
// test-only) so other packages' tests can use it without duplicating a fake.
type FakeClient struct {
	StatusResult    Status
	Projects        []ComposeProject
	ListErr         error
	GetErr          error
	ClosedCallCount int
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

func (f *FakeClient) Close() error {
	f.ClosedCallCount++
	return nil
}
