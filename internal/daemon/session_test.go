package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

var (
	singleWS = domain.Workspace{Root: "/src/api", Kind: domain.WorkspaceSingle,
		Repos: []domain.Repo{{Name: "api", Path: "/src/api", DefaultBranch: "main"}}}
	orchWS = domain.Workspace{Root: "/src/shop", Kind: domain.WorkspaceOrchestration,
		Repos: []domain.Repo{{Name: "web", Path: "/src/shop/web"}}, LastUsed: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
)

type sessionRig struct {
	d    *daemon.Daemon
	c    *rpc.Client
	path string
	host *fakeHost
	wts  *fakeWorktrees
	now  time.Time
}

func startSessions(t *testing.T, store *memStore, setup app.SetupFunc) sessionRig {
	t.Helper()
	r := sessionRig{host: &fakeHost{}, wts: &fakeWorktrees{}, now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	store.snap.Workspaces = append(store.snap.Workspaces, singleWS, orchWS)
	for _, s := range store.snap.Sessions {
		r.host.panes = append(r.host.panes, app.PaneInfo{ID: app.PaneID(s.Pane), Alive: true})
	}
	r.d, r.path = start(t, store,
		daemon.WithHarnesses(r.host, claude.Adapter{}, codex.Adapter{}),
		daemon.WithSessions(r.wts, setup, "/h/worktrees"),
		daemon.WithClock(func() time.Time { return r.now }),
		daemon.WithSlotWatch(10*time.Millisecond))
	r.c = dial(t, r.path)
	return r
}

func (r sessionRig) state(t *testing.T) rpc.State {
	t.Helper()
	sub, err := dial(t, r.path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sub.State
}

func TestNewSessionInASingleRepoStartsInAFreshWorktree(t *testing.T) {
	var setupIn []string
	r := startSessions(t, &memStore{}, func(_ context.Context, dir string) error {
		setupIn = append(setupIn, dir)
		return nil
	})
	var got domain.Session
	params := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "https://linear.app/acme/issue/ENG-1/fix-login", Harness: "claude", Model: "opus", Effort: "high"}
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, params, &got); err != nil {
		t.Fatal(err)
	}
	if want := []addedWorktree{{"/src/api", "/h/worktrees/api/eng-1", "eng-1", "origin/main"}}; !reflect.DeepEqual(r.wts.added, want) {
		t.Fatalf("added %+v", r.wts.added)
	}
	if !slices.Equal(setupIn, []string{"/h/worktrees/api/eng-1"}) {
		t.Fatalf("setup ran in %v", setupIn)
	}
	wantSpec := app.PaneSpec{Name: "eng-1", Dir: "/h/worktrees/api/eng-1",
		Command: []string{"claude", "--model", "opus", "--effort", "high"}}
	if len(r.host.specs) != 1 || !reflect.DeepEqual(r.host.specs[0], wantSpec) {
		t.Fatalf("specs %+v", r.host.specs)
	}
	if got.Pane != "%7" || got.State != domain.StateIdle || got.Harness != domain.HarnessClaude || len(got.WorktreeIDs) != 1 || got.TaskID == "" {
		t.Fatalf("session %+v", got)
	}
	if !got.StartedAt.Equal(r.now) || got.StartModel != "opus" || got.StartEffort != "high" || got.StartWorkspace != "/src/api" {
		t.Fatalf("creation choices %+v", got)
	}

	st := r.state(t)
	if len(st.Tasks) != 1 || st.Tasks[0].ID != got.TaskID || st.Tasks[0].Source != domain.TaskLinear || st.Tasks[0].Ref != "ENG-1" {
		t.Fatalf("tasks %+v", st.Tasks)
	}
	wantWT := domain.Worktree{ID: "/h/worktrees/api/eng-1", Repo: "/src/api", Path: "/h/worktrees/api/eng-1", Branch: "eng-1", SessionID: got.ID}
	if len(st.Worktrees) != 1 || !reflect.DeepEqual(st.Worktrees[0], wantWT) {
		t.Fatalf("worktrees %+v", st.Worktrees)
	}
	for _, w := range st.Workspaces {
		if w.Root == "/src/api" && !w.LastUsed.Equal(r.now) {
			t.Fatalf("workspace not marked used: %+v", w)
		}
	}
}

func TestNewSessionAtAnOrchestrationRootStartsAtTheRoot(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	var got domain.Session
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/shop", WorkItem: "tidy up", Harness: "codex"}, &got); err != nil {
		t.Fatal(err)
	}
	if len(r.wts.added) != 0 || got.WorktreeIDs != nil || got.Harness != domain.HarnessCodex {
		t.Fatalf("added %v, session %+v", r.wts.added, got)
	}
	if len(r.host.specs) != 1 || r.host.specs[0].Dir != "/src/shop" || r.host.specs[0].Command[0] != "codex" {
		t.Fatalf("specs %+v", r.host.specs)
	}
	if st := r.state(t); len(st.Worktrees) != 0 || len(st.Tasks) != 1 || st.Tasks[0].Text != "tidy up" {
		t.Fatalf("state %+v", st)
	}
}

func TestNewSessionWithoutAWorkspaceUsesTheLastUsed(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{WorkItem: "x", Harness: "claude"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.host.specs) != 1 || r.host.specs[0].Dir != "/src/shop" {
		t.Fatalf("specs %+v", r.host.specs)
	}
}

func TestNewSessionOnTheSameWorkItemJoinsItsTaskInANewWorktree(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	var a, b domain.Session
	p := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "https://github.com/acme/api/pull/42", Harness: "claude"}
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &a); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &b); err != nil {
		t.Fatal(err)
	}
	if a.TaskID != b.TaskID || a.ID == b.ID || a.WorktreeIDs[0] == b.WorktreeIDs[0] {
		t.Fatalf("a %+v, b %+v", a, b)
	}
	if len(r.wts.added) != 2 || r.wts.added[1].path != "/h/worktrees/api/api-42-2" {
		t.Fatalf("added %+v", r.wts.added)
	}
	if st := r.state(t); len(st.Tasks) != 1 || len(st.Sessions) != 2 {
		t.Fatalf("state %+v", st)
	}
}

func TestNewSessionErrors(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	var rerr *rpc.Error
	err := r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/nope", WorkItem: "x", Harness: "claude"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("unknown workspace: %v", err)
	}
	err = r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "other"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("unknown harness: %v", err)
	}
	r.wts.err = errors.New("branch exists")
	err = r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "claude"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeFailed {
		t.Fatalf("worktree failure: %v", err)
	}
	if st := r.state(t); len(st.Sessions) != 0 || len(st.Tasks) != 0 {
		t.Fatalf("a failed start left %+v", st)
	}
}

func TestNewSessionWithNoWorkspaceRegistered(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}), daemon.WithSessions(&fakeWorktrees{}, nil, "/h"))
	var rerr *rpc.Error
	err := dial(t, path).Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{WorkItem: "x", Harness: "claude"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("no workspace: %v", err)
	}
}

func TestEndSessionKillsThePaneAndKeepsTheSessionListed(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	var s domain.Session
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	var ended domain.Session
	if err := r.c.Call(context.Background(), rpc.MethodEndSession, rpc.SessionRef{ID: s.ID}, &ended); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.host.killed, []app.PaneID{"%7"}) || ended.State != domain.StateIdle || ended.Pane != "" || !ended.Ended {
		t.Fatalf("killed %v, session %+v", r.host.killed, ended)
	}
	st := r.state(t)
	if len(st.Sessions) != 1 || st.Sessions[0].Pane != "" || !st.Sessions[0].Ended || len(st.Worktrees) != 1 {
		t.Fatalf("state %+v", st)
	}
	var rerr *rpc.Error
	if err := r.c.Call(context.Background(), rpc.MethodEndSession, rpc.SessionRef{ID: "nope"}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("unknown session: %v", err)
	}
}

func startWithClient(t *testing.T, inView string, sessions ...domain.Session) (sessionRig, *fakeClientHost, app.Slot) {
	t.Helper()
	store := &memStore{}
	store.snap.Sessions = sessions
	r := startSessions(t, store, nil)
	clientHost := &fakeClientHost{}
	r.d.SetClientHost(clientHost)
	opened, err := r.c.OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"agentws", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: inView}, nil); err != nil {
		t.Fatal(err)
	}
	r.host.shown, clientHost.focused = nil, nil
	return r, clientHost, app.Slot(opened.Slot)
}

func endSession(t *testing.T, r sessionRig, id string) {
	t.Helper()
	if err := r.c.Call(context.Background(), rpc.MethodEndSession, rpc.SessionRef{ID: id}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestEndingTheSessionInViewShowsTheNextOne(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	endSession(t, r, "a")
	if !reflect.DeepEqual(r.host.shown, []shown{{"%8", slot}}) || len(clientHost.ensured) != 0 {
		t.Fatalf("shown %+v, ensured %v; want b's pane in the slot", r.host.shown, clientHost.ensured)
	}
	if len(clientHost.focused) != 0 {
		t.Fatalf("keyboard focus moved to the agent pane: %v", clientHost.focused)
	}
	focused := map[string]bool{}
	for _, s := range r.state(t).Sessions {
		focused[s.ID] = s.Focused
	}
	if !reflect.DeepEqual(focused, map[string]bool{"b": true}) {
		t.Fatalf("focused %v; want a forgotten and b marked as the one in view", focused)
	}
}

func TestEndingTheLastSessionLeavesAnEmptyStateInTheSlot(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a", domain.Session{ID: "a", Pane: "%7"})
	endSession(t, r, "a")
	if !slices.Equal(clientHost.ensured, []app.Slot{slot}) || len(r.host.shown) != 0 {
		t.Fatalf("shown %+v, ensured %v; want the slot to get its empty-state pane", r.host.shown, clientHost.ensured)
	}
}

func hasSession(t *testing.T, r sessionRig, id string) bool {
	t.Helper()
	for _, s := range r.state(t).Sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

func sessionState(t *testing.T, r sessionRig, id string) domain.Session {
	t.Helper()
	for _, s := range r.state(t).Sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no session %s", id)
	return domain.Session{}
}

func TestAgentExitingInViewShowsTheNextOne(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	r.host.die("%7")
	clientHost.loseSlotPane()
	waitUntil(t, "b to be shown", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		return reflect.DeepEqual(r.host.shown, []shown{{"%8", slot}})
	})
	if hasSession(t, r, "a") {
		t.Fatal("session a is still listed after its agent exited")
	}
	waitUntil(t, "b to be marked in view", func() bool { return sessionState(t, r, "b").Focused })
}

func TestSlotWatchDoesNotEndASessionThatTookTheSlotMeanwhile(t *testing.T) {
	r, clientHost, _ := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	checked := make(chan struct{})
	clientHost.mu.Lock()
	clientHost.duringCheck = func() {
		defer close(checked)
		if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "b"}, nil); err != nil {
			t.Error(err)
		}
	}
	clientHost.mu.Unlock()
	r.host.die("%7")
	clientHost.loseSlotPane()
	<-checked
	time.Sleep(200 * time.Millisecond)
	if b := sessionState(t, r, "b"); b.Pane != "%8" || !b.Focused {
		t.Fatalf("b was ended by a recovery meant for a: %+v", b)
	}
}

func TestQuittingAnEditorInTheSlotPutsTheLiveAgentBack(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	clientHost.loseSlotPane()
	waitUntil(t, "a's agent back in the slot", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		return reflect.DeepEqual(r.host.shown, []shown{{"%7", slot}})
	})
	if a := sessionState(t, r, "a"); a.Pane != "%7" || !a.Focused {
		t.Fatalf("session a was ended though its agent is alive: %+v", a)
	}
	r.host.mu.Lock()
	defer r.host.mu.Unlock()
	if len(r.host.killed) != 0 {
		t.Fatalf("killed %v", r.host.killed)
	}
}

func TestFocusingDuringSlotRecoveryLeavesTheNewSessionShown(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	focused := make(chan struct{})
	r.host.mu.Lock()
	r.host.duringAlive = func() {
		go func() {
			defer close(focused)
			if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "b"}, nil); err != nil {
				t.Error(err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
	}
	r.host.mu.Unlock()
	clientHost.loseSlotPane()
	<-focused
	waitUntil(t, "both the recovery and the focus to show a pane", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		return len(r.host.shown) >= 2
	})
	r.host.mu.Lock()
	last := r.host.shown[len(r.host.shown)-1]
	r.host.mu.Unlock()
	if last != (shown{"%8", slot}) {
		t.Fatalf("recovery showed a over the newly focused b: last shown %+v", last)
	}
	if b := sessionState(t, r, "b"); !b.Focused {
		t.Fatalf("b not focused: %+v", b)
	}
}

func TestLastAgentExitingLeavesAnEmptyStateInTheSlot(t *testing.T) {
	r, clientHost, slot := startWithClient(t, "a", domain.Session{ID: "a", Pane: "%7"})
	r.host.die("%7")
	clientHost.loseSlotPane()
	waitUntil(t, "the empty-state pane", func() bool {
		clientHost.mu.Lock()
		defer clientHost.mu.Unlock()
		return slices.Equal(clientHost.ensured, []app.Slot{slot})
	})
	if hasSession(t, r, "a") {
		t.Fatal("session a is still listed after its agent exited")
	}
}

func TestEndingASessionNotInViewLeavesTheSlotAlone(t *testing.T) {
	r, clientHost, _ := startWithClient(t, "a",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"})
	endSession(t, r, "b")
	if len(r.host.shown) != 0 || len(clientHost.ensured) != 0 {
		t.Fatalf("shown %+v, ensured %v; want the slot untouched", r.host.shown, clientHost.ensured)
	}
}

func TestFocusSessionShowsItsPaneInTheMainSlot(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{
		{ID: "a", Pane: "%7", State: domain.StateDone, Unread: true},
		{ID: "b", Pane: "%8", State: domain.StateRunning, Focused: true},
	}
	r := startSessions(t, store, nil)
	focused := func() map[string]bool {
		out := map[string]bool{}
		for _, s := range r.state(t).Sessions {
			out[s.ID] = s.Focused
		}
		return out
	}
	if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "a"}, nil); err != nil {
		t.Fatalf("focus without a client layout: %v", err)
	}
	if len(r.host.shown) != 0 || !reflect.DeepEqual(focused(), map[string]bool{"a": true, "b": false}) {
		t.Fatalf("without a layout: shown %+v, focused %v", r.host.shown, focused())
	}
	clientHost := &fakeClientHost{}
	r.d.SetClientHost(clientHost)
	opened, err := r.c.OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"agentws", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "b"}, nil); err != nil {
		t.Fatal(err)
	}
	slot := app.Slot(opened.Slot)
	if !reflect.DeepEqual(r.host.shown, []shown{{"%8", slot}}) || !slices.Equal(clientHost.focused, []app.Slot{slot}) {
		t.Fatalf("shown %+v, focused %v", r.host.shown, clientHost.focused)
	}
	if !reflect.DeepEqual(focused(), map[string]bool{"a": false, "b": true}) {
		t.Fatalf("focused %v", focused())
	}
}

func TestRestoredSessionsWhosePaneIsGoneAreEnded(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{
		{ID: "alive", Pane: "%1", State: domain.StateRunning},
		{ID: "gone", Pane: "%2", State: domain.StateWaiting},
	}
	host := &fakeHost{panes: []app.PaneInfo{{ID: "%1", Alive: true}}}
	_, path := start(t, store, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	waitFor(t, func() bool {
		sub, err := c.Subscribe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]domain.Session{}
		for _, s := range sub.State.Sessions {
			byID[s.ID] = s
		}
		_, kept := byID["gone"]
		return !kept &&
			byID["alive"].State == domain.StateRunning && byID["alive"].Pane == "%1"
	})
}

func TestNewSessionRetryAfterAFailedSetupGetsAFreshWorktree(t *testing.T) {
	fail := true
	r := startSessions(t, &memStore{}, func(context.Context, string) error {
		if fail {
			return errors.New("recipe broke")
		}
		return nil
	})
	p := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "fix it", Harness: "claude"}
	var rerr *rpc.Error
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeFailed {
		t.Fatalf("failed setup: %v", err)
	}
	fail = false
	var s domain.Session
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &s); err != nil {
		t.Fatal(err)
	}
	if len(r.wts.added) != 2 || r.wts.added[1].path != "/h/worktrees/api/fix-it-2" {
		t.Fatalf("added %+v", r.wts.added)
	}
	owners := map[string]string{}
	for _, w := range r.state(t).Worktrees {
		owners[w.Path] = w.SessionID
	}
	if want := map[string]string{"/h/worktrees/api/fix-it": "", "/h/worktrees/api/fix-it-2": s.ID}; !reflect.DeepEqual(owners, want) {
		t.Fatalf("worktree owners %v, want %v", owners, want)
	}
}

func TestEndingASessionWithNoWorktreeForgetsIt(t *testing.T) {
	store := &memStore{}
	r := startSessions(t, store, nil)
	var s domain.Session
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/shop", WorkItem: "x", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	sub, err := dial(t, r.path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	endSession(t, r, s.ID)
	for next(t, sub.Diffs).RemovedSession != s.ID {
		continue
	}
	if st := r.state(t); len(st.Sessions) != 0 {
		t.Fatalf("sessions %+v, want none", st.Sessions)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.snap.Sessions) != 0 {
		t.Fatalf("store still has %+v", store.snap.Sessions)
	}
}

func TestEndingAForgottenSessionInViewShowsTheNextRowNotTheFirst(t *testing.T) {
	r, _, slot := startWithClient(t, "b",
		domain.Session{ID: "a", Pane: "%7"},
		domain.Session{ID: "b", Pane: "%8"},
		domain.Session{ID: "c", Pane: "%9"})
	endSession(t, r, "b")
	if !reflect.DeepEqual(r.host.shown, []shown{{"%9", slot}}) {
		t.Fatalf("shown %+v; want c's pane, the row after b", r.host.shown)
	}
}

func TestForgettingASessionDropsItsSubagents(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateRunning}}
	r := startSessions(t, store, nil)
	hookOnPane(t, r.c, "SubagentStart", `{"agent_id":"a1","agent_type":"Explore"}`)
	waitUntil(t, "the subagent to be tracked", func() bool { return len(r.state(t).Subagents) == 1 })
	endSession(t, r, "a")
	if st := r.state(t); len(st.Sessions) != 0 || len(st.Subagents) != 0 {
		t.Fatalf("sessions %+v, subagents %+v; want both gone", st.Sessions, st.Subagents)
	}
}

func TestNewSessionInAFolderNotYetRegisteredRegistersIt(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{},
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithHarnesses(host, claude.Adapter{}),
		daemon.WithSessions(&fakeWorktrees{}, nil, "/h/worktrees"))
	c := dial(t, path)
	var s domain.Session
	if err := c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/solo", WorkItem: "x", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.State.Workspaces) != 1 || sub.State.Workspaces[0].Root != "/solo" || sub.State.Workspaces[0].Kind != domain.WorkspaceSingle {
		t.Fatalf("workspaces %+v; want /solo registered as a single repo", sub.State.Workspaces)
	}
}

func TestNewSessionNormalizesTheWorkspacePath(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{},
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithHarnesses(host, claude.Adapter{}),
		daemon.WithSessions(&fakeWorktrees{}, nil, "/h/worktrees"))
	c := dial(t, path)
	for _, ws := range []string{"/solo/.", "/solo"} {
		var s domain.Session
		if err := c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: ws, WorkItem: "x " + ws, Harness: "claude"}, &s); err != nil {
			t.Fatal(err)
		}
	}
	var rerr *rpc.Error
	if err := c.Call(context.Background(), rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "solo", WorkItem: "y", Harness: "claude"}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("relative workspace = %v; want bad_request", err)
	}
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.State.Workspaces) != 1 || sub.State.Workspaces[0].Root != "/solo" {
		t.Fatalf("workspaces %+v; want one, /solo", sub.State.Workspaces)
	}
}

func TestResumeSessionRelaunchesAnEndedSessionWithTheHarnessIDItLastReported(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	ctx := context.Background()
	var s domain.Session
	if err := r.c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "claude", Model: "opus", Effort: "high"}, &s); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c-1", "c-2"} {
		hook := rpc.Hook{Harness: "claude", Event: "SessionStart", Pane: s.Pane, At: time.Now(), Payload: []byte(`{"session_id":"` + id + `"}`)}
		if err := r.c.Call(ctx, rpc.MethodHook, hook, nil); err != nil {
			t.Fatal(err)
		}
	}
	endSession(t, r, s.ID)
	var resumed domain.Session
	if err := r.c.Call(ctx, rpc.MethodResumeSession, rpc.SessionRef{ID: s.ID}, &resumed); err != nil {
		t.Fatal(err)
	}
	last := r.host.specs[len(r.host.specs)-1]
	if want := []string{"claude", "--resume", "c-2", "--model", "opus", "--effort", "high"}; !slices.Equal(last.Command, want) || last.Dir != s.Dir || s.Dir == "" {
		t.Fatalf("launched %+v, want %v in %q", last, want, s.Dir)
	}
	if resumed.Ended || resumed.Pane != "%7" || resumed.ID != s.ID || !slices.Equal(resumed.WorktreeIDs, s.WorktreeIDs) {
		t.Fatalf("resumed %+v", resumed)
	}
	st := r.state(t)
	if len(st.Sessions) != 1 || st.Sessions[0].Ended || st.Sessions[0].Pane != "%7" {
		t.Fatalf("state %+v", st.Sessions)
	}
}

func TestResumeSessionRefusesALiveOrUnknownSession(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	ctx := context.Background()
	var s domain.Session
	if err := r.c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	var rerr *rpc.Error
	if err := r.c.Call(ctx, rpc.MethodResumeSession, rpc.SessionRef{ID: s.ID}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("live session: %v", err)
	}
	if err := r.c.Call(ctx, rpc.MethodResumeSession, rpc.SessionRef{ID: "nope"}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("unknown session: %v", err)
	}
}

func TestResumeSessionLaunchesOnceWhenTwoClientsResumeTogether(t *testing.T) {
	r := startSessions(t, &memStore{}, nil)
	ctx := context.Background()
	var s domain.Session
	if err := r.c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "x", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	hook := rpc.Hook{Harness: "claude", Event: "SessionStart", Pane: s.Pane, At: time.Now(), Payload: []byte(`{"session_id":"c-1"}`)}
	if err := r.c.Call(ctx, rpc.MethodHook, hook, nil); err != nil {
		t.Fatal(err)
	}
	endSession(t, r, s.ID)
	r.host.mu.Lock()
	before := len(r.host.specs)
	r.host.creating, r.host.createGate = make(chan struct{}, 2), make(chan struct{})
	r.host.mu.Unlock()
	errs := make(chan error, 2)
	resume := func() { errs <- dial(t, r.path).Call(ctx, rpc.MethodResumeSession, rpc.SessionRef{ID: s.ID}, nil) }
	go resume()
	<-r.host.creating
	go resume()
	time.Sleep(100 * time.Millisecond)
	close(r.host.createGate)
	var failed int
	for range 2 {
		if <-errs != nil {
			failed++
		}
	}
	r.host.mu.Lock()
	launched := len(r.host.specs) - before
	r.host.mu.Unlock()
	if launched != 1 || failed != 1 {
		t.Fatalf("launched %d panes, %d calls failed; want 1 and 1", launched, failed)
	}
}
