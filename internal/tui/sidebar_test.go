package tui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestSessionsShowNoWorktreeRows(t *testing.T) {
	st := fixture(1, 3)
	out := screen(newModel(&st, nil))
	for _, absent := range []string{"api:part-1", "web:part-2", "infra:part-3"} {
		if strings.Contains(out, absent) {
			t.Errorf("sidebar lists worktree %q:\n%s", absent, out)
		}
	}
	if !strings.Contains(lineWith(t, out, "opus-5.5"), "3 worktrees") {
		t.Errorf("the selected session does not count its worktrees:\n%s", out)
	}
}

func TestOnlyTheSelectedSessionShowsItsModelLine(t *testing.T) {
	st := fixture(2, 1)
	m := newModel(&st, nil)
	if out := screen(m); !strings.Contains(out, "opus-5.5 · high · 1 worktree") || strings.Contains(out, "gpt-6") {
		t.Fatalf("want only the first session's model line:\n%s", out)
	}
	if out := screen(press(m, "j")); !strings.Contains(out, "gpt-6 · med") || strings.Contains(out, "opus-5.5") {
		t.Fatalf("want only the second session's model line after j:\n%s", out)
	}
}

func TestTheModelLineLeavesOutTheContextFigure(t *testing.T) {
	st := fixture(1, 1)
	if out := screen(newModel(&st, nil)); strings.Contains(out, "ctx") {
		t.Fatalf("sidebar shows the context figure:\n%s", out)
	}
}

func TestASessionWithoutWorktreesSaysItWorksInTheRoot(t *testing.T) {
	st := rpc.State{Sessions: []domain.Session{{ID: "s1", Harness: domain.HarnessClaude, Model: "opus", Effort: "high"}}}
	if out := screen(newModel(&st, nil)); !strings.Contains(out, "opus · high · root only") {
		t.Fatalf("want the model line to say root only:\n%s", out)
	}
}

func TestPortsShowOnTheSessionRow(t *testing.T) {
	st := portsFixture()
	out := screen(newPortsModel(&st, &fakeKiller{}))
	if row := lineWith(t, out, "session 1 change"); !strings.Contains(row, ":3000 :8081 :9229") {
		t.Errorf("session row = %q, want every port of the session, sorted", row)
	}
	if row := lineWith(t, out, "session 2 change"); !strings.Contains(row, ":4000") {
		t.Errorf("unselected session row = %q, want its port", row)
	}
}

func TestFinishedSubagentsCollapseIntoACountOnTheSelectedSession(t *testing.T) {
	st := fixture(2, 1)
	st.Subagents = []domain.Subagent{
		agent("s01", "a1", "Explore", domain.SubagentStopped, "Found 3 callers."),
		agent("s01", "a2", "Plan", domain.SubagentStopped, "Plan is ready."),
		agent("s02", "a3", "Review", domain.SubagentStopped, "Looks fine."),
	}
	out := screen(newModel(&st, nil))
	if !strings.Contains(out, "2 subagents done") {
		t.Errorf("selected session does not count its finished subagents:\n%s", out)
	}
	for _, absent := range []string{"Explore", "Found 3 callers.", "Review", "1 subagent done"} {
		if strings.Contains(out, absent) {
			t.Errorf("screen has %q:\n%s", absent, out)
		}
	}
}

func TestASubagentWithoutATypeShowsNoID(t *testing.T) {
	st := fixture(1, 1)
	st.Subagents = []domain.Subagent{agent("s01", "a8be38f87624eaea6", "", domain.SubagentRunning, "open the PR")}
	out := screen(newModel(&st, nil))
	if strings.Contains(out, "a8be38f87624eaea6") || !strings.Contains(out, "open the PR") {
		t.Fatalf("want the summary and no id:\n%s", out)
	}
}

func TestTaskHeaderShowsTheRefAndTitleWithoutASeparator(t *testing.T) {
	st := fixture(1, 1)
	if out := screen(newModel(&st, nil)); !strings.Contains(out, " #40 task number 1") {
		t.Fatalf("task header:\n%s", out)
	}
}

func TestMouseWheelScrollsTheListWithoutMovingTheSelection(t *testing.T) {
	st := fixture(20, 1)
	m := newModel(&st, nil)
	if strings.Contains(screen(m), "session 20 change") {
		t.Fatalf("fixture fits on screen; the test needs a list that scrolls:\n%s", screen(m))
	}
	m = wheel(m, true, 20)
	if m.Selected() != "s01" {
		t.Fatalf("the wheel moved the selection to %q", m.Selected())
	}
	if out := screen(m); !strings.Contains(out, "session 20 change") || strings.Contains(out, "1 session 1 change") {
		t.Fatalf("the wheel did not scroll to the end of the list:\n%s", out)
	}
	if out := screen(wheel(m, false, 20)); !strings.Contains(out, "1 session 1 change") {
		t.Fatalf("the wheel did not scroll back to the top:\n%s", out)
	}
}

func TestAKeyAfterScrollingBringsTheSelectionBackIntoView(t *testing.T) {
	st := fixture(20, 1)
	m := press(wheel(newModel(&st, nil), true, 20), "j")
	if m.Selected() != "s02" {
		t.Fatalf("j selected %q, want s02", m.Selected())
	}
	if out := screen(m); !strings.Contains(out, "session 2 change") {
		t.Fatalf("the selected session is off screen:\n%s", out)
	}
}

func TestClickingAScrolledRowSelectsItWithoutJumping(t *testing.T) {
	st := fixture(20, 1)
	m := wheel(newModel(&st, nil), true, 20)
	_, before := spot(t, m, "session 19 change")
	m = clickOn(t, m, "session 19 change")
	if m.Selected() != "s19" {
		t.Fatalf("click selected %q, want s19", m.Selected())
	}
	if _, after := spot(t, m, "session 19 change"); after != before {
		t.Fatalf("the list jumped from row %d to %d after the click", before, after)
	}
}

func TestTheSelectionStaysVisibleWhenATallCardLeavesTheListTwoRows(t *testing.T) {
	st := fixture(12, 1)
	for i := range st.Worktrees {
		pr := st.Worktrees[i].PR
		pr.State = domain.PROpen
		for c := range 20 {
			pr.Failing = append(pr.Failing, domain.FailingCheck{Name: fmt.Sprint("check ", c)})
		}
	}
	m := update(newModel(&st, nil), tea.WindowSizeMsg{Width: 48, Height: 32})
	for range 11 {
		m = press(m, "j")
		name := "session " + strings.TrimLeft(strings.TrimPrefix(m.Selected(), "s"), "0") + " change"
		out := screen(m)
		if !slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
			return strings.HasPrefix(l, "▌") && strings.Contains(l, name)
		}) {
			t.Fatalf("selected %s is off screen:\n%s", m.Selected(), out)
		}
	}
}

func TestManyPortsLeaveTheSessionNameVisible(t *testing.T) {
	st := fixture(1, 1)
	for p := range 8 {
		st.Worktrees[0].Ports = append(st.Worktrees[0].Ports, domain.Port{Port: 8000 + p})
	}
	row := lineWith(t, screen(newModel(&st, nil)), ":8000")
	if !strings.Contains(row, "session 1") || !strings.Contains(row, ":8000") || !strings.Contains(row, "+") {
		t.Fatalf("session row = %q, want the name, the first ports and a count of the rest", row)
	}
}
