package daemon_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

var (
	tabProject = domain.Project{Root: "/code/shop", Name: "shop"}
	tabWT      = domain.Worktree{ID: "/wt/api/x", Repo: "/code/shop/api", Path: "/wt/api/x", Branch: "x", SessionID: "host"}
	tabHost    = domain.Session{ID: "host", TaskID: "t1", Pane: "%a1", Harness: domain.HarnessClaude, WorktreeIDs: []string{tabWT.ID}, StartedAt: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	soloWT     = domain.Worktree{ID: "/wt/solo/y", Repo: "/code/solo", Path: "/wt/solo/y", SessionID: "solo"}
	soloHost   = domain.Session{ID: "solo", TaskID: "t2", Pane: "%a2", Harness: domain.HarnessClaude, WorktreeIDs: []string{soloWT.ID}}
)

func startTabs(t *testing.T) termRig {
	t.Helper()
	store := &memStore{}
	store.snap.Sessions = []domain.Session{tabHost, soloHost}
	store.snap.Worktrees = []domain.Worktree{tabWT, soloWT}
	store.snap.Projects = []domain.Project{tabProject}
	store.snap.Tasks = []domain.Task{{ID: "t1", Text: "x"}, {ID: "t2", Text: "y"}}
	r := termRig{client: &fakeClientHost{}, editor: &fakeEditor{}, home: shortDir(t)}
	r.term = newTermFake(r.client, "%a1", "%a2")
	d, path := start(t, store,
		daemon.WithHarnesses(r.term, claude.Adapter{}),
		daemon.WithTerminals(r.home, r.editor))
	d.SetClientHost(r.client)
	r.path = path
	r.c = dial(t, path)
	opened, err := r.c.OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"agentws", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	r.slot = app.Slot(opened.Slot)
	return r
}

func (r termRig) tab(t *testing.T, method string, p rpc.TabParams) (domain.Tab, error) {
	t.Helper()
	var out domain.Tab
	err := r.c.Call(context.Background(), method, p, &out)
	return out, err
}

func inSlot(r termRig) app.PaneID {
	return r.client.ShownIn(context.Background(), r.slot)
}

func TestTabNewShellOpensAShellInTheWorktreeAndShowsIt(t *testing.T) {
	r := startTabs(t)
	tab, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	specs := r.term.createdSpecs()
	if len(specs) != 1 || specs[0].Dir != tabWT.Path || len(specs[0].Command) != 0 {
		t.Fatalf("specs = %+v; want a default shell in %s", specs, tabWT.Path)
	}
	if tab.Kind != domain.TabShell || tab.Pane != "%t1" || inSlot(r) != "%t1" {
		t.Fatalf("tab = %+v, shown %s; want the new shell in the slot", tab, inSlot(r))
	}
	st := eventually(t, r.path, 2*time.Second, "the shell tab in the state", func(st rpc.State) bool {
		return len(st.ShellTabs) == 1 && st.ShellTabs[0].Worktree == tabWT.ID && st.ShellTabs[0].Pane == "%t1"
	})
	if st.ActiveTabs[tabWT.ID] != tab.ID {
		t.Errorf("active tabs = %v; want %s", st.ActiveTabs, tab.ID)
	}
}

func TestTabNewAgentStartsASessionThatOwnsNoWorktree(t *testing.T) {
	r := startTabs(t)
	tab, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "agent", Harness: "claude", Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	specs := r.term.createdSpecs()
	if len(specs) != 1 || specs[0].Dir != tabWT.Path || len(specs[0].Command) == 0 {
		t.Fatalf("specs = %+v; want claude in %s", specs, tabWT.Path)
	}
	st := snapshot(t, r.path)
	got := session(st, tab.ID)
	if got.Tab != tabWT.ID || got.TaskID != "t1" || len(got.WorktreeIDs) != 0 || got.Pane != "%t1" || got.Model != "opus" {
		t.Fatalf("tab session = %+v", got)
	}
	if w, _ := worktree(st, tabWT.ID); w.SessionID != "host" {
		t.Errorf("the worktree moved to %q", w.SessionID)
	}
	if inSlot(r) != "%t1" || !got.Focused {
		t.Errorf("shown %s, focused %v; want the new agent in view", inSlot(r), got.Focused)
	}
}

func TestTabNewRefusesASessionOutsideProjects(t *testing.T) {
	r := startTabs(t)
	_, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "solo", Kind: "shell"})
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("err = %v; want a refusal naming projects", err)
	}
	if len(r.term.createdSpecs()) != 0 {
		t.Error("a pane was created for a session outside projects")
	}
}

func TestTabStepCyclesThroughTheWorktreesTabs(t *testing.T) {
	r := startTabs(t)
	sh, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.tab(t, rpc.MethodTabStep, rpc.TabParams{Session: "host", Delta: 1})
	if err != nil || next.ID != "host" || inSlot(r) != "%a1" {
		t.Fatalf("step = %+v, %v, shown %s; want the host agent back", next, err, inSlot(r))
	}
	back, err := r.tab(t, rpc.MethodTabStep, rpc.TabParams{Session: "host", Delta: -1})
	if err != nil || back.ID != sh.ID || inSlot(r) != app.PaneID(sh.Pane) {
		t.Fatalf("step back = %+v, %v, shown %s", back, err, inSlot(r))
	}
	picked, err := r.tab(t, rpc.MethodTabShow, rpc.TabParams{Session: "host", Tab: "host"})
	if err != nil || picked.ID != "host" || inSlot(r) != "%a1" {
		t.Fatalf("show = %+v, %v, shown %s", picked, err, inSlot(r))
	}
}

func TestTabCloseShellKillsOnlyItsPaneAndShowsANeighbour(t *testing.T) {
	r := startTabs(t)
	sh, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.tab(t, rpc.MethodTabClose, rpc.TabParams{Session: "host"}); err != nil {
		t.Fatal(err)
	}
	if got := r.term.killedPanes(); len(got) != 1 || got[0] != app.PaneID(sh.Pane) {
		t.Fatalf("killed %v; want only the shell", got)
	}
	if inSlot(r) != "%a1" {
		t.Errorf("shown %s; want the host agent", inSlot(r))
	}
	st := eventually(t, r.path, 2*time.Second, "the shell tab gone", func(st rpc.State) bool { return len(st.ShellTabs) == 0 })
	if _, ok := worktree(st, tabWT.ID); !ok || session(st, "host").Ended {
		t.Error("closing a shell tab touched the worktree or the agent")
	}
}

func TestTabCloseOfTheLastAgentKeepsTheWorktreeWithTheProject(t *testing.T) {
	r := startTabs(t)
	if _, err := r.tab(t, rpc.MethodTabShow, rpc.TabParams{Session: "host", Tab: "host"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.tab(t, rpc.MethodTabClose, rpc.TabParams{Session: "host", Tab: "host"}); err != nil {
		t.Fatal(err)
	}
	st := snapshot(t, r.path)
	host := session(st, "host")
	if !host.Ended || len(host.WorktreeIDs) != 1 {
		t.Fatalf("host = %+v; want it ended and still holding its worktree", host)
	}
	if w, ok := worktree(st, tabWT.ID); !ok || w.SessionID != "host" {
		t.Fatalf("worktree = %+v, %v; want it kept", w, ok)
	}
	if got := r.term.killedPanes(); len(got) != 1 || got[0] != "%a1" {
		t.Errorf("killed %v; want the agent pane only", got)
	}
}

func TestTabCloseOfAnAgentTabEndsOnlyThatAgent(t *testing.T) {
	r := startTabs(t)
	tab, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "agent", Harness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.tab(t, rpc.MethodTabClose, rpc.TabParams{Session: tab.ID}); err != nil {
		t.Fatal(err)
	}
	st := snapshot(t, r.path)
	if session(st, "host").Ended || inSlot(r) != "%a1" {
		t.Fatalf("host ended or not shown (%s)", inSlot(r))
	}
	for _, s := range st.Sessions {
		if s.ID == tab.ID && !s.Ended {
			t.Fatalf("the tab agent is still live: %+v", s)
		}
	}
}

func TestTabStripIsInEveryTabPanesTitle(t *testing.T) {
	r := startTabs(t)
	sh, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return strings.HasPrefix(r.term.title("%a1"), "[1 ○ claude]  2 shell ┃ ") &&
			strings.HasPrefix(r.term.title(app.PaneID(sh.Pane)), "1 ○ claude  [2 shell] ┃ ")
	})
	if got := r.term.title("%a2"); strings.Contains(got, "┃") {
		t.Errorf("a session outside projects got a tab strip: %q", got)
	}
}

func TestTabCloseOfTheLastShellClearsTheActiveTab(t *testing.T) {
	r := startTabs(t)
	if _, err := r.tab(t, rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.tab(t, rpc.MethodTabClose, rpc.TabParams{Session: "host", Tab: "host"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.tab(t, rpc.MethodTabClose, rpc.TabParams{Session: "host"}); err != nil {
		t.Fatal(err)
	}
	st := eventually(t, r.path, 2*time.Second, "no shell tabs", func(st rpc.State) bool { return len(st.ShellTabs) == 0 })
	if tab, ok := st.ActiveTabs[tabWT.ID]; ok {
		t.Fatalf("active tab %q after every tab closed", tab)
	}
}

func TestTabShellsGoWithTheirRemovedWorktree(t *testing.T) {
	store := &memStore{}
	store.snap.Projects = []domain.Project{{Root: "/solo", Name: "solo"}}
	store.snap.Sessions = []domain.Session{{ID: "host", Pane: "%7", State: domain.StateIdle, WorktreeIDs: []string{"/solo-feat"}}}
	store.snap.Worktrees = []domain.Worktree{{ID: "/solo-feat", Repo: "/solo", Path: "/solo-feat", Branch: "feat", SessionID: "host"}}
	client := &fakeClientHost{}
	term := newTermFake(client, "%7")
	env := wtEnv{lister: &fakeLister{listings: map[string]domain.RepoListing{}}, finder: &fakeFinder{prs: map[string][]domain.PullRequest{}}}
	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	store.snap.Workspaces = []domain.Workspace{{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo"}}}}
	_, env.path = start(t, store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, env.finder),
		daemon.WithWorktreePoll(50*time.Millisecond, time.Hour),
		daemon.WithHarnesses(term, claude.Adapter{}),
		daemon.WithTerminals(shortDir(t), &fakeEditor{}),
	)
	var tab domain.Tab
	if err := dial(t, env.path).Call(context.Background(), rpc.MethodTabNew, rpc.TabParams{Session: "host", Kind: "shell"}, &tab); err != nil {
		t.Fatal(err)
	}
	env.lister.set("/solo")
	st := eventually(t, env.path, 2*time.Second, "the worktree and its shell gone", func(st rpc.State) bool {
		_, ok := worktree(st, "/solo-feat")
		return !ok && len(st.ShellTabs) == 0
	})
	if _, ok := st.ActiveTabs["/solo-feat"]; ok {
		t.Error("the removed worktree kept an active tab")
	}
	waitFor(t, func() bool { return len(term.killedPanes()) == 1 && term.killedPanes()[0] == app.PaneID(tab.Pane) })
}

func TestTabAgentsBannerNamesItsWorktree(t *testing.T) {
	store := &memStore{}
	store.snap.Worktrees = []domain.Worktree{tabWT}
	r := newRig(t, store, nil)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "tab", Harness: domain.HarnessCodex, Pane: "%9", State: domain.StateRunning, Tab: tabWT.ID}})
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "Stop", "%9", "")
	if b := r.banner(t); !strings.Contains(b.Title, "api@x") {
		t.Fatalf("banner title %q; want the tab's worktree", b.Title)
	}
}

func TestTabAgentsTurnsSnapshotTheWorktreeItIsATabOf(t *testing.T) {
	env := reviewEnv{store: &memStore{}, git: newFakeReviewGit(), lister: &fakeLister{listings: map[string]domain.RepoListing{}}}
	env.store.snap.Workspaces = []domain.Workspace{{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo", DefaultBranch: "main"}}}}
	env.store.snap.Sessions = []domain.Session{
		{ID: "s1", Pane: "%1", State: domain.StateIdle, WorktreeIDs: []string{"/solo-feat"}},
		{ID: "tab", Pane: "%9", State: domain.StateIdle, Tab: "/solo-feat"},
	}
	env.store.snap.Worktrees = []domain.Worktree{{ID: "/solo-feat", Repo: "/solo", Path: "/solo-feat", Branch: "feat", SessionID: "s1"}}
	env.git.trees["/solo-feat"] = "t1"
	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	_, env.path = start(t, env.store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, &fakeFinder{prs: map[string][]domain.PullRequest{}}),
		daemon.WithWorktreePoll(time.Hour, time.Hour),
		daemon.WithReview(env.git),
	)
	payload, _ := json.Marshal(map[string]any{"cwd": "/solo-feat/web", "prompt": "go"})
	h := rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: "%9", At: time.Now(), Payload: payload}
	if err := dial(t, env.path).Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
	ref := domain.TurnRef("tab", "/solo-feat", 1)
	waitUntil(t, "the tab's turn snapshot of its worktree", func() bool { return env.git.refsOf("/solo-feat")[ref] == "t1" })
	st := snapshot(t, env.path)
	if w, _ := worktree(st, "/solo-feat"); w.SessionID != "s1" {
		t.Errorf("the worktree moved to %q", w.SessionID)
	}
}

func TestTabSessionsLeaveTheWorktreesTheyTouchUnassigned(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "tab", Pane: "%2", State: domain.StateRunning, Tab: "/solo-host"}}
	env := startWorktrees(t, store, 50*time.Millisecond)

	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"}, domain.ListedWorktree{Path: "/solo-mine", Branch: "mine"})
	postToolUse(t, env.path, "%2", "/solo-mine", "git worktree add ../solo-feat -b feat")
	eventually(t, env.path, 2*time.Second, "both worktrees listed", func(st rpc.State) bool {
		_, feat := worktree(st, "/solo-feat")
		_, mine := worktree(st, "/solo-mine")
		return feat && mine
	})
	time.Sleep(200 * time.Millisecond)
	st := snapshot(t, env.path)
	for _, id := range []string{"/solo-feat", "/solo-mine"} {
		if w, _ := worktree(st, id); w.SessionID != "" {
			t.Errorf("%s went to %q; a tab session must not own a worktree", id, w.SessionID)
		}
	}
	if ids := session(st, "tab").WorktreeIDs; len(ids) != 0 {
		t.Errorf("tab session holds %v", ids)
	}
}
