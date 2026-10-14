package daemon_test

import (
	"context"
	"sync"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type fakeCleanupWorld struct {
	mu        sync.Mutex
	moved     []string
	dirty     map[string]int
	backedUp  []string
	holdersOf map[string][]string
}

func (f *fakeCleanupWorld) CleanupFacts(_ context.Context, w domain.Worktree) (app.WorktreeGitFacts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return app.WorktreeGitFacts{Fingerprint: "f", Uncommitted: f.dirty[w.Path]}, nil
}

func (f *fakeCleanupWorld) Backup(_ context.Context, w domain.Worktree, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backedUp = append(f.backedUp, w.Path)
	return nil
}

func (f *fakeCleanupWorld) CreateBranch(_ context.Context, _ domain.Worktree, name string) (string, error) {
	return name, nil
}

func (f *fakeCleanupWorld) Prune(context.Context, string) error { return nil }

func (f *fakeCleanupWorld) Holders(_ context.Context, paths []string) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]string{}
	for _, p := range paths {
		if h, ok := f.holdersOf[p]; ok {
			out[p] = h
		}
	}
	return out, nil
}

func (f *fakeCleanupWorld) Move(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, path)
	return nil
}

func (f *fakeCleanupWorld) Purge() {}

func (f *fakeCleanupWorld) Record(app.CleanupRecord) {}

func (f *fakeCleanupWorld) movedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.moved...)
}

func (f *fakeCleanupWorld) backedUpPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.backedUp...)
}
