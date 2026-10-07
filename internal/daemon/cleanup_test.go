package daemon_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type cleanupEnv struct {
	wtEnv
	world *fakeCleanupWorld
}

func startCleanup(t *testing.T, store *memStore, prPoll time.Duration, existing ...domain.ListedWorktree) cleanupEnv {
	t.Helper()
	store.snap.Workspaces = append(store.snap.Workspaces, domain.Workspace{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo"}}})
	store.snap.Sessions = append(store.snap.Sessions,
		domain.Session{ID: "s1", Pane: "%1", State: domain.StateRunning},
		domain.Session{ID: "s2", Pane: "%2", State: domain.StateIdle})
	world := &fakeCleanupWorld{}
	env := cleanupEnv{
		wtEnv: wtEnv{lister: &fakeLister{listings: map[string]domain.RepoListing{}}, finder: &fakeFinder{prs: map[string][]domain.PullRequest{}}},
		world: world,
	}
	env.lister.set("/solo", existing...)
	c := app.NewCleanup(world, world, world, world, "/h/backups", time.Now)
	_, env.path = start(t, store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, env.finder),
		daemon.WithWorktreePoll(time.Hour, prPoll),
		daemon.WithCleanup(c, time.Hour),
	)
	return env
}

func mergedPR(n int, head string) domain.PullRequest {
	return domain.PullRequest{Number: n, Head: head, State: domain.PRMerged}
}

func TestCleanupDryRunPlansWithoutTouchingAnything(t *testing.T) {
	store := &memStore{}
	store.snap.Worktrees = []domain.Worktree{
		{ID: "/solo-a", Repo: "/solo", Path: "/solo-a", Branch: "a", PR: &domain.PullRequest{Number: 1, Head: "a", State: domain.PRMerged}},
		{ID: "/solo-b", Repo: "/solo", Path: "/solo-b", Branch: "b", SessionID: "s1", PR: &domain.PullRequest{Number: 2, Head: "b", State: domain.PRMerged}},
		{ID: "/solo-c", Repo: "/solo", Path: "/solo-c", Branch: "c", SessionID: "s2", PR: &domain.PullRequest{Number: 3, Head: "c", State: domain.PRMerged}},
	}
	env := startCleanup(t, store, time.Hour,
		domain.ListedWorktree{Path: "/solo-a", Branch: "a"}, domain.ListedWorktree{Path: "/solo-b", Branch: "b"}, domain.ListedWorktree{Path: "/solo-c", Branch: "c"})
	items, err := dial(t, env.path).CleanupPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]rpc.CleanupItem{}
	for _, it := range items {
		got[it.Path] = it
	}
	if got["/solo-a"].Action != domain.CleanupRemove || got["/solo-a"].Reason != "PR #1 merged" || got["/solo-a"].Outcome != "" {
		t.Errorf("/solo-a = %+v, want a planned remove", got["/solo-a"])
	}
	if got["/solo-b"].Action != domain.CleanupKeep || got["/solo-b"].Reason != "its session is live" {
		t.Errorf("/solo-b = %+v, want kept for its running session", got["/solo-b"])
	}
	if got["/solo-c"].Action != domain.CleanupRemove {
		t.Errorf("/solo-c = %+v, want remove: its session is idle", got["/solo-c"])
	}
	if moved := env.world.movedPaths(); len(moved) != 0 {
		t.Errorf("dry run moved %v", moved)
	}
}

func TestCleanupSessionActivityWithinGraceKeeps(t *testing.T) {
	store := &memStore{}
	store.snap.Worktrees = []domain.Worktree{{ID: "/solo-c", Repo: "/solo", Path: "/solo-c", Branch: "c", SessionID: "s2", PR: &domain.PullRequest{Number: 3, Head: "c", State: domain.PRMerged}}}
	store.snap.Events = []domain.SessionEvent{{SessionID: "s2", At: time.Now().Add(-time.Minute), Kind: domain.EventStop}}
	env := startCleanup(t, store, time.Hour, domain.ListedWorktree{Path: "/solo-c", Branch: "c"})
	items, err := dial(t, env.path).CleanupPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != domain.CleanupKeep {
		t.Errorf("items = %+v, want kept for recent session activity", items)
	}
}

func TestCleanupRunRemovesAndReportsOutcomes(t *testing.T) {
	store := &memStore{}
	store.snap.Worktrees = []domain.Worktree{{ID: "/solo-a", Repo: "/solo", Path: "/solo-a", Branch: "a", PR: &domain.PullRequest{Number: 1, Head: "a", State: domain.PRMerged}}}
	env := startCleanup(t, store, time.Hour, domain.ListedWorktree{Path: "/solo-a", Branch: "a"})
	items, err := dial(t, env.path).CleanupRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Outcome != "removed" {
		t.Errorf("items = %+v", items)
	}
	if moved := env.world.movedPaths(); !slices.Equal(moved, []string{"/solo-a"}) {
		t.Errorf("moved = %v", moved)
	}
}

func TestCleanupRunsWhenAPRIsSeenMerged(t *testing.T) {
	env := startCleanup(t, &memStore{}, 100*time.Millisecond, domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	eventually(t, env.path, 2*time.Second, "adopted", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-feat")
		return ok
	})
	env.finder.set("/solo", domain.PullRequest{Number: 42, Head: "feat", State: domain.PROpen})
	eventually(t, env.path, 2*time.Second, "open PR", func(st rpc.State) bool {
		w, _ := worktree(st, "/solo-feat")
		return w.PR != nil
	})
	time.Sleep(250 * time.Millisecond)
	if moved := env.world.movedPaths(); len(moved) != 0 {
		t.Fatalf("moved %v while the PR was open", moved)
	}
	env.finder.set("/solo", mergedPR(42, "feat"))
	deadline := time.Now().Add(2 * time.Second)
	for !slices.Equal(env.world.movedPaths(), []string{"/solo-feat"}) {
		if time.Now().After(deadline) {
			t.Fatalf("moved = %v, want /solo-feat once its PR merged", env.world.movedPaths())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCleanupKeepsTheWorktreesOfAProjectAfterTheirSessionEnded(t *testing.T) {
	store := &memStore{}
	store.snap.Projects = []domain.Project{{Root: "/solo", Name: "solo"}}
	store.snap.Worktrees = []domain.Worktree{{ID: "/h/solo-a", Repo: "/solo", Path: "/h/solo-a", Branch: "a", SessionID: "s2", PR: &domain.PullRequest{Number: 1, Head: "a", State: domain.PRMerged}}}
	env := startCleanup(t, store, time.Hour, domain.ListedWorktree{Path: "/h/solo-a", Branch: "a"})
	items, err := dial(t, env.path).CleanupRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != domain.CleanupKeep || items[0].Reason != "held by project solo" {
		t.Errorf("items = %+v, want kept for the project", items)
	}
	if moved := env.world.movedPaths(); len(moved) != 0 {
		t.Errorf("moved = %v", moved)
	}
}
