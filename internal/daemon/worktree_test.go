package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

var soloFS = fakeFS{markers: map[string]domain.GitMarker{"/solo": domain.GitDir}}

type wtEnv struct {
	path   string
	lister *fakeLister
	finder *fakeFinder
}

func startWorktrees(t *testing.T, store *memStore, poll time.Duration, existing ...domain.ListedWorktree) wtEnv {
	t.Helper()
	store.snap.Workspaces = append(store.snap.Workspaces, domain.Workspace{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo"}}})
	store.snap.Sessions = append(store.snap.Sessions, domain.Session{ID: "s1", Pane: "%1", State: domain.StateRunning})
	env := wtEnv{lister: &fakeLister{listings: map[string]domain.RepoListing{}}, finder: &fakeFinder{prs: map[string][]domain.PullRequest{}}}
	env.lister.set("/solo", append(existing, domain.ListedWorktree{Path: "/solo-base"})...)
	_, env.path = start(t, store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, env.finder),
		daemon.WithWorktreePoll(poll, poll),
	)
	eventually(t, env.path, 2*time.Second, "baseline scan", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-base")
		return ok
	})
	return env
}

func snapshot(t *testing.T, path string) rpc.State {
	t.Helper()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sub.State
}

func eventually(t *testing.T, path string, within time.Duration, what string, ok func(rpc.State) bool) rpc.State {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		st := snapshot(t, path)
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v; state %+v", what, within, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func worktree(st rpc.State, id string) (domain.Worktree, bool) {
	for _, w := range st.Worktrees {
		if w.ID == id {
			return w, true
		}
	}
	return domain.Worktree{}, false
}

func session(st rpc.State, id string) domain.Session {
	for _, s := range st.Sessions {
		if s.ID == id {
			return s
		}
	}
	return domain.Session{}
}

func postToolUse(t *testing.T, path, pane, cwd, command string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"cwd": cwd, "tool_name": "Bash", "tool_input": map[string]string{"command": command}})
	c := dial(t, path)
	h := rpc.Hook{Harness: "claude", Event: "PostToolUse", Pane: pane, At: time.Now(), Payload: payload}
	if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeDetectExistingWorktreesAreAdoptedUnassigned(t *testing.T) {
	env := startWorktrees(t, &memStore{}, time.Hour, domain.ListedWorktree{Path: "/solo-old", Branch: "old"})
	st := eventually(t, env.path, 2*time.Second, "adopted", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-old")
		return ok
	})
	w, _ := worktree(st, "/solo-old")
	if w.SessionID != "" || w.Repo != "/solo" || w.Branch != "old" {
		t.Errorf("adopted = %+v, want unassigned in /solo", w)
	}
}

func TestWorktreeDetectHookAttachesNewWorktreeToItsSession(t *testing.T) {
	env := startWorktrees(t, &memStore{}, time.Hour)

	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	postToolUse(t, env.path, "%1", "/solo", "git worktree add ../solo-feat -b feat")

	st := eventually(t, env.path, 2*time.Second, "attached", func(st rpc.State) bool {
		w, ok := worktree(st, "/solo-feat")
		return ok && w.SessionID == "s1"
	})
	if ids := session(st, "s1").WorktreeIDs; len(ids) != 1 || ids[0] != "/solo-feat" {
		t.Errorf("session worktrees = %v", ids)
	}
}

func TestWorktreeDetectWorktreeInAnUnregisteredRepoFoundFromSessionCwd(t *testing.T) {
	env := startWorktrees(t, &memStore{}, time.Hour)
	env.lister.set("/other")
	postToolUse(t, env.path, "%1", "/other", "ls")
	time.Sleep(50 * time.Millisecond)

	env.lister.set("/other", domain.ListedWorktree{Path: "/other-x", Branch: "x"})
	postToolUse(t, env.path, "%1", "/other", "git worktree add ../other-x")
	eventually(t, env.path, 2*time.Second, "attached", func(st rpc.State) bool {
		w, ok := worktree(st, "/other-x")
		return ok && w.SessionID == "s1" && w.Repo == "/other"
	})
}

func TestWorktreeDetectPollFindsHandMadeAndDropsRemoved(t *testing.T) {
	env := startWorktrees(t, &memStore{}, 100*time.Millisecond, domain.ListedWorktree{Path: "/solo-old", Branch: "old"})
	eventually(t, env.path, 2*time.Second, "adopted", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-old")
		return ok
	})

	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-hand", Branch: "hand"})
	st := eventually(t, env.path, 2*time.Second, "polled", func(st rpc.State) bool {
		_, hand := worktree(st, "/solo-hand")
		_, old := worktree(st, "/solo-old")
		return hand && !old
	})
	if w, _ := worktree(st, "/solo-hand"); w.SessionID != "" {
		t.Errorf("hand-made worktree = %+v, want unassigned", w)
	}
}

func TestWorktreeDetectRemovalDetachesFromSessionAndStore(t *testing.T) {
	store := &memStore{}
	store.snap.Worktrees = []domain.Worktree{{ID: "/solo-a", Repo: "/solo", Path: "/solo-a", Branch: "a", SessionID: "s1"}}
	env := startWorktrees(t, store, 100*time.Millisecond)
	st := eventually(t, env.path, 2*time.Second, "removed", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-a")
		return !ok
	})
	if ids := session(st, "s1").WorktreeIDs; len(ids) != 0 {
		t.Errorf("session still lists %v", ids)
	}
	for _, w := range store.snap.Worktrees {
		if w.ID == "/solo-a" {
			t.Errorf("store still has %+v", w)
		}
	}
}

func TestWorktreeDetectPRAndChecksAppearOnNextPoll(t *testing.T) {
	env := startWorktrees(t, &memStore{}, 100*time.Millisecond, domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	eventually(t, env.path, 2*time.Second, "adopted", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-feat")
		return ok
	})
	env.finder.set("/solo", domain.PullRequest{Number: 42, Head: "feat", State: domain.PROpen, Checks: domain.CheckPending})
	eventually(t, env.path, 2*time.Second, "pr", func(st rpc.State) bool {
		w, _ := worktree(st, "/solo-feat")
		return w.PR != nil && w.PR.Number == 42 && w.PR.Checks == domain.CheckPending
	})
}

func TestPRBoardPollBacksOffWhileGitHubFails(t *testing.T) {
	env := startWorktrees(t, &memStore{}, 40*time.Millisecond)
	env.finder.fail(errors.New("rate limited"))
	before := env.finder.callCount()
	time.Sleep(700 * time.Millisecond)
	if n := env.finder.callCount() - before; n > 6 {
		t.Errorf("%d polls in 700ms while failing, want the interval to grow", n)
	}
}

func TestPRBoardPollRecoversOnceGitHubAnswers(t *testing.T) {
	env := startWorktrees(t, &memStore{}, 40*time.Millisecond, domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	env.finder.fail(errors.New("boom"))
	time.Sleep(200 * time.Millisecond)
	env.finder.fail(nil)
	env.finder.set("/solo", domain.PullRequest{Number: 7, Head: "feat", State: domain.PROpen, Checks: domain.CheckPassing})
	eventually(t, env.path, 3*time.Second, "pr after recovery", func(st rpc.State) bool {
		w, _ := worktree(st, "/solo-feat")
		return w.PR != nil && w.PR.Number == 7
	})
}

func TestPRBoardPollSpeedsUpWhileChecksRun(t *testing.T) {
	env := startWorktrees(t, &memStore{}, 800*time.Millisecond, domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	env.finder.set("/solo", domain.PullRequest{Number: 7, Head: "feat", State: domain.PROpen, Checks: domain.CheckPending})
	start := time.Now()
	for env.finder.callCount() < 4 {
		if time.Since(start) > 1900*time.Millisecond {
			t.Fatalf("%d polls in %v, want at least 4 (a quarter of the interval while checks run)", env.finder.callCount(), time.Since(start))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWorktreeDetectAssign(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "s2", Pane: "%2"}}
	env := startWorktrees(t, store, time.Hour, domain.ListedWorktree{Path: "/solo-old", Branch: "old"})
	eventually(t, env.path, 2*time.Second, "adopted", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-old")
		return ok
	})
	c := dial(t, env.path)
	ctx := context.Background()
	if err := c.WorktreeAssign(ctx, "/solo-old", "s1"); err != nil {
		t.Fatal(err)
	}
	if err := c.WorktreeAssign(ctx, "/solo-old", "s2"); err != nil {
		t.Fatal(err)
	}
	st := snapshot(t, env.path)
	if w, _ := worktree(st, "/solo-old"); w.SessionID != "s2" {
		t.Errorf("owner = %q", w.SessionID)
	}
	if ids := session(st, "s1").WorktreeIDs; len(ids) != 0 {
		t.Errorf("s1 still lists %v", ids)
	}
	if ids := session(st, "s2").WorktreeIDs; len(ids) != 1 {
		t.Errorf("s2 lists %v", ids)
	}
	var rerr *rpc.Error
	if err := c.WorktreeAssign(ctx, "/nope", "s1"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown worktree: %v", err)
	}
	if err := c.WorktreeAssign(ctx, "/solo-old", "nobody"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v", err)
	}
	if err := c.WorktreeAssign(ctx, "/solo-old", ""); err != nil {
		t.Errorf("unassign: %v", err)
	}
}

func TestWorktreeDetectRemovingTheLastWorktreeOfAnEndedSessionForgetsIt(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "s2", State: domain.StateIdle, Ended: true, WorktreeIDs: []string{"/solo-b"}}}
	store.snap.Worktrees = []domain.Worktree{{ID: "/solo-b", Repo: "/solo", Path: "/solo-b", Branch: "b", SessionID: "s2"}}
	env := startWorktrees(t, store, 100*time.Millisecond)
	st := eventually(t, env.path, 2*time.Second, "forgotten", func(st rpc.State) bool {
		for _, s := range st.Sessions {
			if s.ID == "s2" {
				return false
			}
		}
		return true
	})
	if s := session(st, "s1"); s.ID != "s1" {
		t.Fatalf("the live session went too: %+v", st.Sessions)
	}
}
