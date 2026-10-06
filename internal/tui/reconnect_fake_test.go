package tui_test

import (
	"context"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

type fakeDaemon struct {
	*fakeCaller
	*fakeFocuser
	*fakeAttender
	*fakeKiller
	*fakeSwitcher
	*fakeReviewer
	*fakeOnboarder
	disk *fakeDisker
}

func newFakeDaemon() *fakeDaemon {
	return &fakeDaemon{
		fakeCaller: &fakeCaller{}, fakeFocuser: &fakeFocuser{}, fakeAttender: &fakeAttender{},
		fakeKiller: &fakeKiller{}, fakeSwitcher: &fakeSwitcher{}, fakeReviewer: &fakeReviewer{},
		fakeOnboarder: &fakeOnboarder{}, disk: &fakeDisker{views: []rpc.DiskView{{}}},
	}
}

func (f *fakeDaemon) DiskView(ctx context.Context) (rpc.DiskView, error) { return f.disk.DiskView(ctx) }

func (f *fakeDaemon) CleanupWorktree(ctx context.Context, path string, backup bool) (rpc.CleanupItem, error) {
	return f.disk.CleanupWorktree(ctx, path, backup)
}

func (f *fakeDaemon) WorktreeShell(ctx context.Context, id string) error {
	return f.disk.WorktreeShell(ctx, id)
}

type redialResult struct {
	conn tui.Connection
	err  error
}

type fakeRedialer struct {
	mu      sync.Mutex
	results []redialResult
	calls   int
}

func (f *fakeRedialer) Redial(context.Context) (tui.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := min(f.calls, len(f.results)-1)
	f.calls++
	return f.results[i].conn, f.results[i].err
}

func (f *fakeRedialer) dials() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
