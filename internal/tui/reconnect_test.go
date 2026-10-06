package tui_test

import (
	"net"
	"strings"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

var refused = &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}

func reconnectingModel(f *fakeRedialer, calls *fakeCaller, canRestart bool) tui.Model {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: calls, Redial: f.Redial, CanRestart: canRestart})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(fixture(1, 0)))
}

func step(t *testing.T, m tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

func redial(t *testing.T, m tui.Model) (tui.Model, tea.Cmd) {
	t.Helper()
	m, cmd := step(t, m, tui.RedialMsg{})
	if cmd == nil {
		t.Fatal("a due retry does not dial")
	}
	return step(t, m, cmd())
}

func TestDisconnectedSidebarRetriesWithBackoffAndShowsTheNextTry(t *testing.T) {
	f := &fakeRedialer{results: []redialResult{{err: refused}}}
	m, cmd := step(t, reconnectingModel(f, &fakeCaller{}, true), tui.DisconnectedMsg{})
	if cmd == nil {
		t.Fatal("a lost subscription schedules no retry")
	}
	out := screen(m)
	for _, want := range []string{"daemon disconnected", "reconnecting · try 1 in 1s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q while disconnected:\n%s", want, out)
		}
	}
	dialing, _ := step(t, m, tui.RedialMsg{})
	if out := screen(dialing); !strings.Contains(out, "reconnecting · try 1…") {
		t.Fatalf("a dial in flight is not shown:\n%s", out)
	}
	if !quitsOn(t, dialing, key("q")) {
		t.Fatal("q does not quit while a dial is in flight")
	}
	for i, want := range []string{"try 2 in 1s", "try 3 in 2s", "try 4 in 4s", "try 5 in 5s", "try 6 in 5s"} {
		m, cmd = redial(t, m)
		if cmd == nil {
			t.Fatalf("failed try %d schedules no retry", i+1)
		}
		if out := screen(m); !strings.Contains(out, "reconnecting · "+want) {
			t.Fatalf("after failed try %d, want %q:\n%s", i+1, want, out)
		}
	}
	if f.dials() != 5 {
		t.Fatalf("dialed %d times for 5 due retries", f.dials())
	}
}

func TestReconnectedSidebarLoadsTheNewStateAndFollowsItsDiffs(t *testing.T) {
	d := newFakeDaemon()
	diffs := make(chan rpc.Diff, 1)
	f := &fakeRedialer{results: []redialResult{
		{err: refused},
		{conn: tui.Connection{State: fixture(3, 0), Diffs: diffs, Daemon: d}},
	}}
	old := &fakeCaller{}
	m, _ := step(t, reconnectingModel(f, old, true), tui.DisconnectedMsg{})
	m, _ = redial(t, m)
	m, listen := redial(t, m)
	out := screen(m)
	if strings.Contains(out, "disconnected") || strings.Contains(out, "reconnecting") {
		t.Fatalf("still shows the disconnect after reconnecting:\n%s", out)
	}
	if !strings.Contains(out, "3 sessions") {
		t.Fatalf("the new daemon's state is not loaded:\n%s", out)
	}
	if listen == nil {
		t.Fatal("the new subscription's diffs are not followed")
	}
	diffs <- rpc.Diff{Session: &domain.Session{ID: "s04", TaskID: "t1", Harness: domain.HarnessClaude, State: domain.StateRunning}}
	m, listen = step(t, m, listen())
	if out := screen(m); !strings.Contains(out, "4 sessions") {
		t.Fatalf("a diff from the new daemon is not applied:\n%s", out)
	}
	if listen == nil {
		t.Fatal("stops following diffs after the first one")
	}
	_, leave := step(t, m, key("q"))
	leave()
	if len(old.calls) != 0 || len(d.methods()) != 1 || d.methods()[0] != rpc.MethodClientDetach {
		t.Fatalf("q after reconnecting called old %v, new %v; want client.detach on the new connection", old.methods(), d.methods())
	}
	close(diffs)
	m, retry := step(t, m, listen())
	if out := screen(m); !strings.Contains(out, "daemon disconnected") {
		t.Fatalf("losing the new daemon is not shown:\n%s", out)
	}
	if retry == nil {
		t.Fatal("losing the new daemon schedules no retry")
	}
}

func TestDaemonOfAnotherBuildStopsRetryingAndOffersARestart(t *testing.T) {
	f := &fakeRedialer{results: []redialResult{{err: rpc.Mismatch("v2", "v1", 2, 1)}}}
	m, _ := step(t, reconnectingModel(f, &fakeCaller{}, true), tui.DisconnectedMsg{})
	m, cmd := redial(t, m)
	if cmd != nil {
		t.Fatal("keeps retrying a daemon of another build")
	}
	out := screen(m)
	for _, want := range []string{"agentws was upgraded", "r restart", "q quit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q on a version mismatch:\n%s", want, out)
		}
	}
	if strings.Contains(out, "reconnecting") {
		t.Fatalf("still says reconnecting after a version mismatch:\n%s", out)
	}
	if !quitsOn(t, m, key("q")) {
		t.Fatal("q does not quit after a version mismatch")
	}
	next, restart := step(t, m, key("r"))
	if restart == nil {
		t.Fatal("r does nothing after a version mismatch")
	}
	if _, ok := restart().(tea.QuitMsg); !ok || !next.Restart() {
		t.Fatal("r does not quit to restart on the new binary")
	}
	if f.dials() != 1 {
		t.Fatalf("dialed %d times; want one", f.dials())
	}
}

func TestDaemonOfAnotherBuildWithoutRestartSaysToRunAgentwsAgain(t *testing.T) {
	f := &fakeRedialer{results: []redialResult{{err: rpc.Mismatch("v2", "v1", 2, 1)}}}
	m, _ := step(t, reconnectingModel(f, &fakeCaller{}, false), tui.DisconnectedMsg{})
	m, _ = redial(t, m)
	out := screen(m)
	if !strings.Contains(out, "agentws was upgraded") || !strings.Contains(out, "q quit, then run agentws") {
		t.Fatalf("no upgrade hint without a restart:\n%s", out)
	}
	if strings.Contains(out, "r restart") {
		t.Fatalf("offers a restart it cannot do:\n%s", out)
	}
	if next, _ := step(t, m, key("r")); next.Restart() {
		t.Fatal("r asks for a restart that was not offered")
	}
}
