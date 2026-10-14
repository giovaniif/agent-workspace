package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const reviewDiff = "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n"

type reviewEnv struct {
	path   string
	store  *memStore
	git    *fakeReviewGit
	lister *fakeLister
}

func startReview(t *testing.T, poll time.Duration) reviewEnv {
	t.Helper()
	env := reviewEnv{store: &memStore{}, git: newFakeReviewGit(), lister: &fakeLister{listings: map[string]domain.RepoListing{}}}
	env.store.snap.Workspaces = []domain.Workspace{{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo", DefaultBranch: "main"}}}}
	env.store.snap.Sessions = []domain.Session{{ID: "s1", Pane: "%1", State: domain.StateIdle, WorktreeIDs: []string{"/solo-feat"}}}
	env.store.snap.Worktrees = []domain.Worktree{{ID: "/solo-feat", Repo: "/solo", Path: "/solo-feat", Branch: "feat", SessionID: "s1"}}
	env.git.trees["/solo-feat"] = "t1"
	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	_, env.path = start(t, env.store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, &fakeFinder{prs: map[string][]domain.PullRequest{}}),
		daemon.WithWorktreePoll(poll, time.Hour),
		daemon.WithReview(env.git),
	)
	return env
}

func prompt(t *testing.T, path string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"cwd": "/solo", "prompt": "go"})
	h := rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: "%1", At: time.Now(), Payload: payload}
	if err := dial(t, path).Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 2s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReviewPromptSnapshotsTheSessionsWorktrees(t *testing.T) {
	env := startReview(t, time.Hour)
	prompt(t, env.path)
	ref := domain.TurnRef("s1", "/solo-feat", 1)
	waitUntil(t, "turn snapshot", func() bool { return env.git.refsOf("/solo-feat")[ref] == "t1" })
}

func TestReviewOpenShowsTheLastTurnAndResetsViewedMarksOnChange(t *testing.T) {
	env := startReview(t, time.Hour)
	c := dial(t, env.path)
	ctx := context.Background()
	prompt(t, env.path)
	waitUntil(t, "turn snapshot", func() bool { return len(env.git.refsOf("/solo-feat")) == 1 })
	env.git.set(func(g *fakeReviewGit) {
		g.trees["/solo-feat"] = "t2"
		g.diffs["t1..t2"] = reviewDiff
	})

	rv, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeLastTurn})
	if err != nil {
		t.Fatal(err)
	}
	if len(rv.Worktrees) != 1 || rv.Worktrees[0].Worktree.ID != "/solo-feat" || len(rv.Worktrees[0].Files) != 1 || rv.Worktrees[0].Files[0].Path != "a.go" {
		t.Fatalf("review = %+v", rv)
	}
	file := rv.Worktrees[0].Files[0]
	if err := c.MarkViewed(ctx, domain.ViewedMark{Worktree: "/solo-feat", Path: "a.go", Blob: file.Blob}, true); err != nil {
		t.Fatal(err)
	}
	rv, err = c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeLastTurn})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.IsViewed(marks(rv.Viewed), "/solo-feat", rv.Worktrees[0].Files[0]) {
		t.Errorf("a.go is not viewed after marking: %+v", rv.Viewed)
	}
	if len(stored(env)) != 1 {
		t.Errorf("stored marks = %+v", stored(env))
	}

	env.git.set(func(g *fakeReviewGit) {
		g.trees["/solo-feat"] = "t3"
		g.diffs["t1..t3"] = "diff --git a/a.go b/a.go\nindex 1..9 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+z\n"
	})
	rv, err = c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeLastTurn})
	if err != nil {
		t.Fatal(err)
	}
	if domain.IsViewed(marks(rv.Viewed), "/solo-feat", rv.Worktrees[0].Files[0]) {
		t.Error("a.go changed again but is still viewed")
	}

	if err := c.MarkViewed(ctx, domain.ViewedMark{Worktree: "/solo-feat", Path: "a.go"}, false); err != nil {
		t.Fatal(err)
	}
	rv, _ = c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeLastTurn})
	if len(rv.Viewed) != 0 || len(stored(env)) != 0 {
		t.Errorf("unmarked yet viewed = %+v, stored %+v", rv.Viewed, stored(env))
	}
}

func TestReviewOpenOneWorktreeOrAnUnknownSession(t *testing.T) {
	env := startReview(t, time.Hour)
	c := dial(t, env.path)
	ctx := context.Background()
	env.git.set(func(g *fakeReviewGit) { g.diffs["base-of-origin/main..t1"] = reviewDiff })

	rv, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeBranch, Worktree: "/solo-feat"})
	if err != nil || len(rv.Worktrees) != 1 || rv.Worktrees[0].Err != "" || len(rv.Worktrees[0].Files) != 1 {
		t.Fatalf("branch review = %+v, %v", rv, err)
	}
	if rv, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeBranch, Worktree: "/elsewhere"}); err != nil || len(rv.Worktrees) != 0 {
		t.Errorf("a worktree the session does not own = %+v, %v", rv, err)
	}
	var rerr *rpc.Error
	if _, err := c.Review(ctx, rpc.ReviewParams{Session: "nope", Scope: domain.ScopeBranch}); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v", err)
	}
}

func TestReviewWithoutAScopeUsesTheLastOneOpenedForTheSession(t *testing.T) {
	env := startReview(t, time.Hour)
	c := dial(t, env.path)
	ctx := context.Background()
	fresh, err := c.Review(ctx, rpc.ReviewParams{Session: "s1"})
	if err != nil || fresh.Scope != domain.ScopeUncommitted {
		t.Fatalf("a session never reviewed gets scope %q, %v; want uncommitted", fresh.Scope, err)
	}
	if _, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	again, err := c.Review(ctx, rpc.ReviewParams{Session: "s1"})
	if err != nil || again.Scope != domain.ScopeBranch {
		t.Fatalf("scope %q after a branch review, %v; want branch", again.Scope, err)
	}
}

func TestReviewRefusesAnUnknownScopeAndKeepsTheSavedOne(t *testing.T) {
	env := startReview(t, time.Hour)
	c := dial(t, env.path)
	ctx := context.Background()
	if _, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: domain.ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Review(ctx, rpc.ReviewParams{Session: "s1", Scope: "typo"})
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("err = %v; want bad_request for an unknown scope", err)
	}
	again, err := c.Review(ctx, rpc.ReviewParams{Session: "s1"})
	if err != nil || again.Scope != domain.ScopeBranch {
		t.Fatalf("scope %q after a refused one, %v; want the saved branch scope", again.Scope, err)
	}
}

func TestReviewRemovedWorktreeDropsItsTurns(t *testing.T) {
	env := startReview(t, 20*time.Millisecond)
	keep := domain.TurnRef("s1", "/solo", 1)
	env.git.set(func(g *fakeReviewGit) {
		g.refs["/solo"] = map[string]string{domain.TurnRef("s1", "/solo-feat", 3): "t", keep: "t"}
	})
	env.lister.set("/solo")
	waitUntil(t, "turns dropped", func() bool {
		refs := env.git.refsOf("/solo")
		_, kept := refs[keep]
		return len(refs) == 1 && kept
	})
}

func TestReviewLayoutWidensTheSidebarAndPutsItBack(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	var rerr *rpc.Error
	if err := c.ReviewLayout(ctx, true); !errors.As(err, &rerr) {
		t.Fatalf("review layout before any client = %v; want an rpc error", err)
	}
	if _, err := c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"agentws", "tui"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.ReviewLayout(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := c.ReviewLayout(ctx, false); err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(host.wide, want) {
		t.Errorf("widen calls = %v, want %v", host.wide, want)
	}
}

func stored(env reviewEnv) []domain.ViewedMark {
	snap, _ := env.store.Load()
	return snap.Viewed
}

func marks(ms []domain.ViewedMark) map[string]domain.ViewedMark {
	out := map[string]domain.ViewedMark{}
	for _, m := range ms {
		out[m.Key()] = m
	}
	return out
}
