package main

import (
	"bytes"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestPrintWorkspacesShowsFactsOnlyOnceRefreshed(t *testing.T) {
	list := rpc.WorkspaceList{
		LastUsed: "/w/shop",
		Workspaces: []domain.Workspace{
			{Root: "/w/shop", Kind: domain.WorkspaceOrchestration, Repos: []domain.Repo{
				{Name: "api", Path: "/w/shop/api", Branch: "feat", DefaultBranch: "main", ChangedFiles: 3},
				{Name: "web", Path: "/w/shop/web", Branch: "main", DefaultBranch: "main"},
				{Name: "new", Path: "/w/shop/new"},
			}},
			{Root: "/w/solo", Kind: domain.WorkspaceSingle},
		},
	}
	var out bytes.Buffer
	printWorkspaces(&out, list)
	want := "/w/shop (orchestration, last used)\n" +
		"  api  feat  main  3 changed\n" +
		"  web  main  main  clean\n" +
		"  new  -     -     -\n" +
		"/w/solo (single)\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}
