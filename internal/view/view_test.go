package view_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/view"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/view -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func nativeFixture() rpc.State {
	return rpc.State{
		Seq:        10,
		Workspaces: []domain.Workspace{{Root: "/w/api"}},
		Tasks: []domain.Task{
			{ID: "t1", Source: domain.TaskText, Text: "add login"},
			{ID: "t2", Source: domain.TaskText, Text: "fix retry"},
		},
		Worktrees: []domain.Worktree{
			{ID: "w1", Repo: "api", Path: "/w/api-feat", Branch: "feat", SessionID: "s1", Ports: []domain.Port{{Port: 3000, PID: 7, PGID: 7, Command: "node"}}},
			{ID: "w2", Repo: "web", Path: "/w/web-fix", Branch: "fix", SessionID: "s2"},
		},
		Sessions: []domain.Session{
			{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, State: domain.StateRunning, WorktreeIDs: []string{"w1"}},
			{ID: "s2", TaskID: "t2", Harness: domain.HarnessCodex, State: domain.StateIdle, WorktreeIDs: []string{"w2"}},
			{ID: "s3", TaskID: "t1", Harness: domain.HarnessClaude, State: domain.StatePermission},
		},
		Events: []domain.SessionEvent{
			{SessionID: "s3", At: t0, Kind: domain.EventPreToolUse, Tool: "Bash", Detail: "make test"},
		},
		Subagents: []domain.Subagent{
			{SessionID: "s1", ID: "a1", Type: "Explore", State: domain.SubagentRunning, StartedAt: t0},
		},
		Reclaimable: rpc.Reclaimable{Size: 2048, Pending: 1},
	}
}

func TestViewSubscribeStateGolden(t *testing.T) {
	_, state := view.NewNative(nativeFixture())
	golden(t, "view-subscribe-state.json", state)
}

func TestViewSubscribeDiffsGolden(t *testing.T) {
	v, _ := view.NewNative(nativeFixture())
	pr := &domain.PullRequest{Number: 12, Title: "Add login", URL: "https://github.com/o/api/pull/12", Head: "feat", State: domain.PROpen, Checks: domain.CheckFailing, Failing: []domain.FailingCheck{{Name: "test", URL: "https://ci/1"}}}
	withPR := domain.Worktree{ID: "w1", Repo: "api", Path: "/w/api-feat", Branch: "feat", SessionID: "s1", PR: pr, Ports: []domain.Port{{Port: 3000, PID: 7, PGID: 7, Command: "node"}}}
	waiting := domain.Session{ID: "s2", TaskID: "t2", Harness: domain.HarnessCodex, State: domain.StateWaiting, WorktreeIDs: []string{"w2"}}
	var out []*view.NativeDiff
	for _, d := range []rpc.Diff{
		{Seq: 11, Worktree: &withPR},
		{Seq: 12, Event: &domain.SessionEvent{SessionID: "s2", At: t0.Add(time.Minute), Kind: domain.EventStop, Text: "Retry added."}},
		{Seq: 13, Session: &waiting},
		{Seq: 14, Subagent: &domain.Subagent{SessionID: "s1", ID: "a1", Type: "Explore", State: domain.SubagentStopped, StartedAt: t0, StoppedAt: t0.Add(time.Minute)}},
		{Seq: 15, RemovedSession: "s3"},
		{Seq: 16, Reclaimable: &rpc.Reclaimable{Size: 4096}},
	} {
		out = append(out, v.ApplyNative(d)...)
	}
	golden(t, "view-subscribe-diffs.json", out)
}

func TestViewSubscribeRenamesASessionWhenItsPRAppears(t *testing.T) {
	v, state := view.NewNative(nativeFixture())
	if state.Sessions[0].Name != "add login" {
		t.Fatalf("name before the PR %q", state.Sessions[0].Name)
	}
	withPR := domain.Worktree{ID: "w1", Repo: "api", Branch: "feat", SessionID: "s1", PR: &domain.PullRequest{Number: 12, Title: "Add login form", State: domain.PROpen}}
	var renamed *view.NativeSession
	for _, d := range v.ApplyNative(rpc.Diff{Seq: 11, Worktree: &withPR}) {
		if d.Session != nil && d.Session.ID == "s1" {
			renamed = d.Session
		}
	}
	if renamed == nil || renamed.Name != domain.NameFor(domain.Task{ID: "t1", Source: domain.TaskText, Text: "add login"}, []domain.PullRequest{*withPR.PR}) {
		t.Fatalf("no session diff with the PR's name: %+v", renamed)
	}
	if len(renamed.Board) != 1 || renamed.Board[0].Number != 12 {
		t.Fatalf("board %+v", renamed.Board)
	}
}

func TestViewSubscribeOrderMatchesTheSidebar(t *testing.T) {
	states := []domain.AgentState{domain.StateRunning, domain.StateWaiting, domain.StateIdle, domain.StatePermission, domain.StateRunning, domain.StateWaiting}
	var st rpc.State
	for i, s := range states {
		id := string(rune('a' + i))
		st.Sessions = append(st.Sessions, domain.Session{ID: id, TaskID: id, State: s})
	}
	st.Sessions = append(st.Sessions, domain.Session{ID: "z", TaskID: "z", State: domain.StateWaiting, Ended: true})
	_, state := view.NewNative(st)
	var flat []domain.Session
	for _, s := range st.Sessions {
		s.TaskID = ""
		flat = append(flat, s)
	}
	want := map[string]int{"z": -1}
	for _, g := range domain.Sidebar(nil, flat) {
		for i, s := range g.Sessions {
			want[s.ID] = i
		}
	}
	for _, s := range state.Sessions {
		if s.Order != want[s.ID] {
			t.Fatalf("session %s (%s) order %d, want %d", s.ID, s.State, s.Order, want[s.ID])
		}
	}
	if want["b"] != 0 || want["a"] != 3 {
		t.Fatalf("sidebar order %v does not put the sessions that need you first", want)
	}
}

func TestViewSubscribeSendsTheOtherSessionsWhoseOrderChanged(t *testing.T) {
	st := rpc.State{Sessions: []domain.Session{
		{ID: "a", TaskID: "a", State: domain.StateRunning},
		{ID: "b", TaskID: "b", State: domain.StateRunning},
	}}
	v, _ := view.NewNative(st)
	got := map[string]int{}
	for _, d := range v.ApplyNative(rpc.Diff{Seq: 2, Session: &domain.Session{ID: "b", TaskID: "b", State: domain.StateWaiting}}) {
		if d.Session != nil {
			got[d.Session.ID] = d.Session.Order
		}
	}
	if len(got) != 2 || got["b"] != 0 || got["a"] != 1 {
		t.Fatalf("orders sent %v", got)
	}
}

func TestViewForThePhoneDropsPortsAndOrder(t *testing.T) {
	_, state := view.New(nativeFixture())
	b, _ := json.Marshal(state)
	for _, key := range []string{`"order"`, `"board"`, `"events"`, `"Ports":[{`} {
		if bytes.Contains(b, []byte(key)) {
			t.Fatalf("phone state has %s: %s", key, b)
		}
	}
}
