package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/giovaniif/crew/internal/domain"
)

const refreshParallelism = 4

type WorkspaceFS interface {
	Marker(path string) (domain.GitMarker, error)
	Children(path string) ([]domain.Child, error)
}

type RepoFacts struct {
	DefaultBranch string
	Branch        string
	ChangedFiles  int
}

type RepoInspector interface {
	Inspect(ctx context.Context, path string) (RepoFacts, error)
}

func DiscoverWorkspace(fs WorkspaceFS, root string, known []domain.Repo) (domain.Workspace, error) {
	marker, err := fs.Marker(root)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("%s: %w", root, err)
	}
	ws := domain.Workspace{Root: root, Kind: domain.KindOfRoot(marker)}
	if ws.Kind == domain.WorkspaceSingle {
		ws.Repos = domain.SingleRepo(root)
	} else {
		children, err := fs.Children(root)
		if err != nil {
			return domain.Workspace{}, fmt.Errorf("%s: %w", root, err)
		}
		ws.Repos = domain.ReposIn(children)
	}
	ws.Repos = domain.MergeRepoState(known, ws.Repos)
	return ws, nil
}

func RefreshRepoFacts(ctx context.Context, git RepoInspector, ws domain.Workspace) domain.Workspace {
	repos := make([]domain.Repo, len(ws.Repos))
	copy(repos, ws.Repos)
	var wg sync.WaitGroup
	sem := make(chan struct{}, refreshParallelism)
	for i := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			facts, err := git.Inspect(ctx, repos[i].Path)
			if err != nil {
				return
			}
			repos[i].DefaultBranch = facts.DefaultBranch
			repos[i].Branch = facts.Branch
			repos[i].ChangedFiles = facts.ChangedFiles
		}()
	}
	wg.Wait()
	ws.Repos = repos
	return ws
}
