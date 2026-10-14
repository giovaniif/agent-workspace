package app_test

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type fakeCleanupGit struct {
	mu             sync.Mutex
	calls          map[string]int
	failAfterFirst bool
	later          map[string]app.WorktreeGitFacts
	facts          map[string]app.WorktreeGitFacts
	backupErr      error
	taken          map[string]bool
	ops            []string
}

func (g *fakeCleanupGit) CleanupFacts(_ context.Context, w domain.Worktree) (app.WorktreeGitFacts, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.calls == nil {
		g.calls = map[string]int{}
	}
	g.calls[w.Path]++
	if g.failAfterFirst && g.calls[w.Path] > 1 {
		return app.WorktreeGitFacts{}, errors.New("git status failed")
	}
	if f, ok := g.later[w.Path]; ok && g.calls[w.Path] > 1 {
		return f, nil
	}
	f, ok := g.facts[w.Path]
	if !ok {
		return app.WorktreeGitFacts{}, errors.New("git status failed")
	}
	return f, nil
}

func (g *fakeCleanupGit) Backup(_ context.Context, w domain.Worktree, dir string) error {
	if g.backupErr != nil {
		return g.backupErr
	}
	g.ops = append(g.ops, "backup "+w.Path+" "+dir)
	return nil
}

func (g *fakeCleanupGit) CreateBranch(_ context.Context, w domain.Worktree, name string) (string, error) {
	got := name
	for i := 2; g.taken[got]; i++ {
		got = name + "-" + strconv.Itoa(i)
	}
	g.ops = append(g.ops, "branch "+w.Path+" "+got)
	return got, nil
}

func (g *fakeCleanupGit) Prune(_ context.Context, repo string) error {
	g.ops = append(g.ops, "prune "+repo)
	return nil
}

type fakeProcs struct {
	calls []map[string][]string
	err   error
	n     int
}

func (p *fakeProcs) Holders(_ context.Context, paths []string) (map[string][]string, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := map[string][]string{}
	if len(p.calls) == 0 {
		return out, nil
	}
	i := min(p.n, len(p.calls)-1)
	p.n++
	for _, path := range paths {
		if h, ok := p.calls[i][path]; ok {
			out[path] = h
		}
	}
	return out, nil
}

type fakeTrash struct {
	purges  int
	moved   []string
	failFor map[string]bool
}

func (t *fakeTrash) Move(path string) error {
	if t.failFor[path] {
		return errors.New("cross-device link")
	}
	t.moved = append(t.moved, path)
	return nil
}

func (t *fakeTrash) Purge() { t.purges++ }

type fakeAudit struct{ records []app.CleanupRecord }

func (a *fakeAudit) Record(r app.CleanupRecord) { a.records = append(a.records, r) }
