package app_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestWorktreeDetectScanListsEachRepoOnce(t *testing.T) {
	api := domain.RepoListing{Main: "/w/api", Worktrees: []domain.ListedWorktree{{Path: "/w/api-a", Branch: "a"}}}
	web := domain.RepoListing{Main: "/w/web"}
	lister := fakeLister{"/w/web": web, "/w/api": api, "/w/api-a/src": api}
	got := app.ScanWorktrees(context.Background(), lister, []string{"/w/web", "/w/api", "/w/api-a/src", "/w/broken"})
	want := []domain.RepoListing{api, web}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWorktreeDetectRefreshPRsReturnsOnlyChanges(t *testing.T) {
	finder := &fakeFinder{prs: map[string][]domain.PullRequest{
		"/w/api": {
			{Number: 5, Head: "a", State: domain.PROpen, Checks: domain.CheckPending},
			{Number: 6, Head: "b", State: domain.PROpen},
		},
		"/w/web": {{Number: 2, Head: "x"}},
	}}
	worktrees := []domain.Worktree{
		{ID: "/w/api-a", Repo: "/w/api", Branch: "a"},
		{ID: "/w/api-b", Repo: "/w/api", Branch: "b", PR: &domain.PullRequest{Number: 6, Head: "b", State: domain.PROpen}},
		{ID: "/w/api-c", Repo: "/w/api", Branch: "c", PR: &domain.PullRequest{Number: 1, Head: "c"}},
		{ID: "/w/web-x", Repo: "/w/web", Branch: "x", PR: &domain.PullRequest{Number: 2, Head: "x"}},
	}
	got, err := app.RefreshPRs(context.Background(), finder, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Worktree{
		{ID: "/w/api-a", Repo: "/w/api", Branch: "a", PR: &domain.PullRequest{Number: 5, Head: "a", State: domain.PROpen, Checks: domain.CheckPending}},
		{ID: "/w/api-c", Repo: "/w/api", Branch: "c"},
	}
	if !reflect.DeepEqual(got.Changed, want) {
		t.Errorf("got %+v, want %+v", got.Changed, want)
	}
	if !reflect.DeepEqual(finder.calls, [][]string{{"/w/api", "/w/web"}}) {
		t.Errorf("calls = %v, want one call naming both repos", finder.calls)
	}
}

func TestPRBoardRefreshAsksOncePerPollWhateverThePRCount(t *testing.T) {
	prs := map[string][]domain.PullRequest{}
	var worktrees []domain.Worktree
	for _, repo := range []string{"/w/a", "/w/b", "/w/c"} {
		for i := range 20 {
			branch := fmt.Sprintf("b%d", i)
			prs[repo] = append(prs[repo], domain.PullRequest{Number: i + 1, Head: branch, State: domain.PROpen})
			worktrees = append(worktrees, domain.Worktree{ID: fmt.Sprintf("%s-%d", repo, i), Repo: repo, Branch: branch})
		}
	}
	finder := &fakeFinder{prs: prs}
	got, err := app.RefreshPRs(context.Background(), finder, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.calls) != 1 {
		t.Errorf("%d requests for %d worktrees, want 1", len(finder.calls), len(worktrees))
	}
	if len(got.Changed) != len(worktrees) {
		t.Errorf("changed %d, want %d", len(got.Changed), len(worktrees))
	}
}

func TestPRBoardRefreshFailureKeepsPRsAndReportsTheError(t *testing.T) {
	finder := &fakeFinder{err: errors.New("rate limited")}
	worktrees := []domain.Worktree{{ID: "/w/api-a", Repo: "/w/api", Branch: "a", PR: &domain.PullRequest{Number: 5, Head: "a"}}}
	got, err := app.RefreshPRs(context.Background(), finder, worktrees)
	if err == nil {
		t.Fatal("want the finder's error")
	}
	if len(got.Changed) != 0 {
		t.Errorf("changed %+v after a failure", got.Changed)
	}
}

func TestPRBoardRefreshReportsChecksRunningOnOpenPRs(t *testing.T) {
	tests := []struct {
		name string
		pr   domain.PullRequest
		want bool
	}{
		{"open and pending", domain.PullRequest{Head: "a", State: domain.PROpen, Checks: domain.CheckPending}, true},
		{"open and passing", domain.PullRequest{Head: "a", State: domain.PROpen, Checks: domain.CheckPassing}, false},
		{"merged with stale pending", domain.PullRequest{Head: "a", State: domain.PRMerged, Checks: domain.CheckPending}, false},
	}
	for _, tc := range tests {
		finder := &fakeFinder{prs: map[string][]domain.PullRequest{"/w/api": {tc.pr}}}
		worktrees := []domain.Worktree{{ID: "/w/api-a", Repo: "/w/api", Branch: "a"}}
		got, err := app.RefreshPRs(context.Background(), finder, worktrees)
		if err != nil {
			t.Fatal(err)
		}
		if got.ChecksRunning != tc.want {
			t.Errorf("%s: ChecksRunning = %v, want %v", tc.name, got.ChecksRunning, tc.want)
		}
	}
}

func TestPRBoardRefreshKeepsPRsOfARepoTheFinderCouldNotResolve(t *testing.T) {
	finder := &fakeFinder{prs: map[string][]domain.PullRequest{"/w/api": {{Number: 5, Head: "a", State: domain.PROpen}}}}
	worktrees := []domain.Worktree{
		{ID: "/w/api-a", Repo: "/w/api", Branch: "a"},
		{ID: "/w/local-x", Repo: "/w/local", Branch: "x", PR: &domain.PullRequest{Number: 9, Head: "x"}},
	}
	got, err := app.RefreshPRs(context.Background(), finder, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changed) != 1 || got.Changed[0].ID != "/w/api-a" {
		t.Errorf("changed %+v, want only /w/api-a", got.Changed)
	}
}
