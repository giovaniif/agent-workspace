package daemon_test

import (
	"context"
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
