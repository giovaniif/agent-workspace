package app

import (
	"context"
	"sync"

	"github.com/giovaniif/crew/internal/domain"
)

type ReviewGit interface {
	WorkingTree(ctx context.Context, dir string) (string, error)
	PointRef(ctx context.Context, dir, ref, tree string) error
	TurnRefs(ctx context.Context, dir string) ([]string, error)
	DeleteRefs(ctx context.Context, dir string, refs []string) error
	Resolve(ctx context.Context, dir, rev string) (string, error)
	MergeBase(ctx context.Context, dir, rev string) (string, error)
	Diff(ctx context.Context, dir, from, tree string) (string, error)
}

func SnapshotTurn(ctx context.Context, g ReviewGit, session, dir string) (string, error) {
	tree, err := g.WorkingTree(ctx, dir)
	if err != nil {
		return "", err
	}
	refs, err := g.TurnRefs(ctx, dir)
	if err != nil {
		return "", err
	}
	_, n := domain.LatestTurn(refs, session, dir)
	ref := domain.TurnRef(session, dir, n+1)
	if err := g.PointRef(ctx, dir, ref, tree); err != nil {
		return "", err
	}
	return ref, g.DeleteRefs(ctx, dir, domain.OlderTurns(refs, session, dir, n+1))
}

func DropTurns(ctx context.Context, g ReviewGit, repoDir, worktree string) error {
	refs, err := g.TurnRefs(ctx, repoDir)
	if err != nil {
		return err
	}
	return g.DeleteRefs(ctx, repoDir, domain.TurnsOfWorktree(refs, worktree))
}

type ReviewTarget struct {
	Session       string
	Worktree      domain.Worktree
	DefaultBranch string
}

const diffCacheSize = 128

type Reviewer struct {
	git   ReviewGit
	mu    sync.Mutex
	cache map[string][]domain.FileDiff
	order []string
}

func NewReviewer(g ReviewGit) *Reviewer {
	return &Reviewer{git: g, cache: map[string][]domain.FileDiff{}}
}

func (r *Reviewer) Review(ctx context.Context, scope domain.ReviewScope, targets []ReviewTarget) []domain.WorktreeReview {
	out := make([]domain.WorktreeReview, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, refreshParallelism)
	for i := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = domain.WorktreeReview{Worktree: targets[i].Worktree}
			from, files, err := r.one(ctx, scope, targets[i])
			if err != nil {
				out[i].Err = err.Error()
				return
			}
			out[i].From, out[i].Files = from, files
		}()
	}
	wg.Wait()
	return out
}

func (r *Reviewer) one(ctx context.Context, scope domain.ReviewScope, t ReviewTarget) (string, []domain.FileDiff, error) {
	dir := t.Worktree.Path
	tree, err := r.git.WorkingTree(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	facts := domain.RangeFacts{DefaultBranch: t.DefaultBranch}
	if scope == domain.ScopeLastTurn {
		refs, err := r.git.TurnRefs(ctx, dir)
		if err != nil {
			return "", nil, err
		}
		facts.LatestTurn, _ = domain.LatestTurn(refs, t.Session, dir)
	}
	rng, err := domain.RangeFor(scope, facts)
	if err != nil {
		return "", nil, err
	}
	var from string
	if rng.MergeBase {
		from, err = r.git.MergeBase(ctx, dir, rng.From)
	} else {
		from, err = r.git.Resolve(ctx, dir, rng.From)
	}
	if err != nil {
		return "", nil, err
	}
	key := from + ".." + tree
	if files, ok := r.cached(key); ok {
		return from, files, nil
	}
	out, err := r.git.Diff(ctx, dir, from, tree)
	if err != nil {
		return "", nil, err
	}
	files := domain.ParseDiff(out)
	r.store(key, files)
	return from, files, nil
}

func (r *Reviewer) cached(key string) ([]domain.FileDiff, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	files, ok := r.cache[key]
	return files, ok
}

func (r *Reviewer) store(key string, files []domain.FileDiff) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cache[key]; ok {
		return
	}
	if len(r.order) >= diffCacheSize {
		delete(r.cache, r.order[0])
		r.order = r.order[1:]
	}
	r.cache[key] = files
	r.order = append(r.order, key)
}
