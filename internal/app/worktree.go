package app

import (
	"context"
	"reflect"
	"sort"
	"sync"

	"github.com/giovaniif/crew/internal/domain"
)

type WorktreeLister interface {
	ListWorktrees(ctx context.Context, dir string) (domain.RepoListing, error)
}

type PRFinder interface {
	PRs(ctx context.Context, repos []string) (map[string][]domain.PullRequest, error)
}

func ScanWorktrees(ctx context.Context, lister WorktreeLister, dirs []string) []domain.RepoListing {
	listings := make([]*domain.RepoListing, len(dirs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, refreshParallelism)
	for i := range dirs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if l, err := lister.ListWorktrees(ctx, dirs[i]); err == nil {
				listings[i] = &l
			}
		}()
	}
	wg.Wait()
	byMain := map[string]domain.RepoListing{}
	for _, l := range listings {
		if l != nil {
			byMain[l.Main] = *l
		}
	}
	out := make([]domain.RepoListing, 0, len(byMain))
	for _, l := range byMain {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Main < out[j].Main })
	return out
}

type PRRefresh struct {
	Changed       []domain.Worktree
	ChecksRunning bool
}

func RefreshPRs(ctx context.Context, finder PRFinder, worktrees []domain.Worktree) (PRRefresh, error) {
	var repos []string
	seen := map[string]bool{}
	for _, w := range worktrees {
		if !seen[w.Repo] {
			seen[w.Repo] = true
			repos = append(repos, w.Repo)
		}
	}
	if len(repos) == 0 {
		return PRRefresh{}, nil
	}
	found, err := finder.PRs(ctx, repos)
	if err != nil {
		return PRRefresh{}, err
	}
	var out PRRefresh
	for _, w := range worktrees {
		prs, ok := found[w.Repo]
		if !ok {
			if running(w.PR) {
				out.ChecksRunning = true
			}
			continue
		}
		pr := domain.PRForBranch(prs, w.Branch)
		if running(pr) {
			out.ChecksRunning = true
		}
		if !reflect.DeepEqual(pr, w.PR) {
			w.PR = pr
			out.Changed = append(out.Changed, w)
		}
	}
	return out, nil
}

func running(pr *domain.PullRequest) bool {
	return pr != nil && pr.State == domain.PROpen && pr.Checks == domain.CheckPending
}
