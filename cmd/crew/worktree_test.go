package main

import (
	"bytes"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

func TestWorktreeDetectPrintWorktrees(t *testing.T) {
	wts := []domain.Worktree{
		{ID: "/w/api-feat", Repo: "/w/api", Path: "/w/api-feat", Branch: "feat", SessionID: "s1",
			PR: &domain.PullRequest{Number: 42, State: domain.PROpen, Checks: domain.CheckPassing}},
		{ID: "/w/api-x", Repo: "/w/api", Path: "/w/api-x"},
		{ID: "/w/web-y", Repo: "/w/web", Path: "/w/web-y", Branch: "y", PR: &domain.PullRequest{Number: 7, State: domain.PRMerged}},
	}
	var out bytes.Buffer
	printWorktrees(&out, wts)
	want := "/w/api-feat  feat  s1          #42 OPEN passing\n" +
		"/w/api-x     -     unassigned  -\n" +
		"/w/web-y     y     unassigned  #7 MERGED\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}
