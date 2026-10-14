package app_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestDiscoveryWorkspaceSingleRepo(t *testing.T) {
	fs := fakeFS{markers: map[string]domain.GitMarker{"/w/api": domain.GitDir}}
	ws, err := app.DiscoverWorkspace(fs, "/w/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Workspace{Root: "/w/api", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "api", Path: "/w/api"}}}
	if !reflect.DeepEqual(ws, want) {
		t.Errorf("workspace = %+v, want %+v", ws, want)
	}
}

func TestDiscoveryWorkspaceOrchestrationRoot(t *testing.T) {
	fs := fakeFS{
		markers: map[string]domain.GitMarker{"/w": domain.GitNone},
		children: map[string][]domain.Child{"/w": {
			{Name: "web", Path: "/w/web", Git: domain.GitDir},
			{Name: "api", Path: "/w/api", Git: domain.GitDir},
			{Name: "api-wt", Path: "/w/api-wt", Git: domain.GitFile},
		}},
	}
	known := []domain.Repo{{Name: "api", Path: "/w/api", Branch: "feat", ChangedFiles: 2}}
	ws, err := app.DiscoverWorkspace(fs, "/w", known)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != domain.WorkspaceOrchestration {
		t.Errorf("kind = %q", ws.Kind)
	}
	want := []domain.Repo{{Name: "api", Path: "/w/api", Branch: "feat", ChangedFiles: 2}, {Name: "web", Path: "/w/web"}}
	if !reflect.DeepEqual(ws.Repos, want) {
		t.Errorf("repos = %+v, want %+v", ws.Repos, want)
	}
}

func TestDiscoveryWorkspaceMissingPath(t *testing.T) {
	if _, err := app.DiscoverWorkspace(fakeFS{}, "/nope", nil); err == nil {
		t.Fatal("want an error for a path that is not a directory")
	}
}

func TestDiscoveryRefreshRepoFacts(t *testing.T) {
	ws := domain.Workspace{Root: "/w", Repos: []domain.Repo{
		{Name: "api", Path: "/w/api"},
		{Name: "broken", Path: "/w/broken", Branch: "old", DefaultBranch: "main", ChangedFiles: 1},
		{Name: "web", Path: "/w/web"},
	}}
	git := fakeGit{
		"/w/api": {DefaultBranch: "main", Branch: "feat", ChangedFiles: 4},
		"/w/web": {DefaultBranch: "master", Branch: "master"},
	}
	got := app.RefreshRepoFacts(context.Background(), git, ws)
	want := []domain.Repo{
		{Name: "api", Path: "/w/api", DefaultBranch: "main", Branch: "feat", ChangedFiles: 4},
		{Name: "broken", Path: "/w/broken", Branch: "old", DefaultBranch: "main", ChangedFiles: 1},
		{Name: "web", Path: "/w/web", DefaultBranch: "master", Branch: "master"},
	}
	if !reflect.DeepEqual(got.Repos, want) {
		t.Errorf("repos = %+v, want %+v", got.Repos, want)
	}
	if ws.Repos[0].Branch != "" {
		t.Error("RefreshRepoFacts modified its input")
	}
}
