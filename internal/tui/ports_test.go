package tui_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func newPortsModel(st *rpc.State, k tui.Killer) tui.Model {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Focus: &fakeFocuser{}, Kill: k})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(*st))
}

func portsFixture() rpc.State {
	st := fixture(2, 2)
	st.Worktrees[0].Ports = []domain.Port{{Port: 8081, PID: 101, PGID: 100, Command: "node"}}
	st.Worktrees[1].Ports = []domain.Port{{Port: 3000, PID: 201, PGID: 200, Command: "bun"}, {Port: 9229, PID: 101, PGID: 100, Command: "node"}}
	st.Worktrees[2].Ports = []domain.Port{{Port: 4000, PID: 301, PGID: 300, Command: "go"}}
	return st
}

func lineWith(t *testing.T, screen, needle string) string {
	t.Helper()
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no line with %q:\n%s", needle, screen)
	return ""
}

func TestPortsShowOnceWhenIPv4AndIPv6ListenOnIt(t *testing.T) {
	st := fixture(1, 1)
	st.Worktrees[0].Ports = []domain.Port{{Port: 8081, PID: 1, PGID: 1}, {Port: 8081, PID: 2, PGID: 2}}
	out := screen(newPortsModel(&st, &fakeKiller{}))
	if header := lineWith(t, out, "session 1 change"); strings.Count(header, ":8081") != 1 {
		t.Errorf("session header = %q, want :8081 once", header)
	}
}

func TestPortsShowInTheStatusLineForTheSelectedSession(t *testing.T) {
	st := portsFixture()
	m := newPortsModel(&st, &fakeKiller{})
	lines := strings.Split(screen(m), "\n")
	status := lines[len(lines)-1]
	if !strings.Contains(status, ":3000 :8081 :9229") || strings.Contains(status, ":4000") {
		t.Errorf("status line = %q", status)
	}
	m = press(m, "j")
	lines = strings.Split(screen(m), "\n")
	if status := lines[len(lines)-1]; !strings.Contains(status, ":4000") || strings.Contains(status, ":8081") {
		t.Errorf("status line after j = %q", status)
	}
}

func TestPortsKillAsksBeforeKilling(t *testing.T) {
	st := portsFixture()
	k := &fakeKiller{}
	m := newPortsModel(&st, k)

	m = press(m, "K")
	lines := strings.Split(screen(m), "\n")
	if prompt := lines[len(lines)-1]; !strings.Contains(prompt, "kill") || !strings.Contains(prompt, ":3000 :8081 :9229") || !strings.Contains(prompt, "y/n") {
		t.Errorf("prompt = %q", prompt)
	}
	if len(k.killed) != 0 {
		t.Fatalf("killed before the answer: %v", k.killed)
	}

	_, cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	cmd()
	if want := [][]int{{100, 200}}; !reflect.DeepEqual(k.killed, want) {
		t.Errorf("killed %v, want %v: one call, each group once", k.killed, want)
	}
}

func TestPortsKillAnyOtherKeyCancels(t *testing.T) {
	st := portsFixture()
	k := &fakeKiller{}
	m := press(newPortsModel(&st, k), "K", "n")
	lines := strings.Split(screen(m), "\n")
	if strings.Contains(lines[len(lines)-1], "y/n") {
		t.Error("prompt still showing after n")
	}
	if _, cmd := m.Update(key("y")); cmd != nil {
		t.Error("y after cancelling still killed")
	}
	if len(k.killed) != 0 {
		t.Errorf("killed %v", k.killed)
	}
}

func TestPortsKillWithoutPortsDoesNothing(t *testing.T) {
	st := fixture(1, 1)
	k := &fakeKiller{}
	m := press(newPortsModel(&st, k), "K")
	if strings.Contains(screen(m), "y/n") {
		t.Error("asked to kill with no ports")
	}
	if _, cmd := m.Update(key("y")); cmd != nil {
		t.Error("y killed with no prompt")
	}
}

func TestPortsKillFailureShowsInTheStatusLine(t *testing.T) {
	st := portsFixture()
	k := &fakeKiller{err: errors.New("failed: survived")}
	m := press(newPortsModel(&st, k), "K")
	_, cmd := m.Update(key("y"))
	m = update(m, cmd())
	lines := strings.Split(screen(m), "\n")
	if !strings.Contains(lines[len(lines)-1], "survived") {
		t.Errorf("status line = %q", lines[len(lines)-1])
	}
}

func TestPortsMoveKeysStillMoveWhileNotAsking(t *testing.T) {
	st := portsFixture()
	m := press(newPortsModel(&st, &fakeKiller{}), "j", "k")
	if m.Selected() != "s01" {
		t.Errorf("selected %q, want s01: k still moves up", m.Selected())
	}
}
