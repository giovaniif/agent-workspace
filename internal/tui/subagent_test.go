package tui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func agent(session, id, kind string, state domain.SubagentState, summary string) domain.Subagent {
	return domain.Subagent{SessionID: session, ID: id, Type: kind, State: state, Summary: summary}
}

func subagentFixture() rpc.State {
	st := fixture(3, 1)
	st.Subagents = []domain.Subagent{
		agent("s02", "a1", "Explore", domain.SubagentStopped, "Found 3 callers."),
		agent("s02", "a2", "Plan", domain.SubagentRunning, ""),
		{SessionID: "s02", ID: "a3", ParentID: "a2", Type: "Reviewer", State: domain.SubagentRunning},
	}
	return st
}

func TestRunningSubagentsShowAsATree(t *testing.T) {
	st := subagentFixture()
	out := screen(newModel(&st, nil))
	lines := strings.Split(out, "\n")
	indent := func(label string) int {
		for _, l := range lines {
			if i := strings.Index(l, label); i >= 0 {
				return i
			}
		}
		t.Fatalf("no line with %q:\n%s", label, out)
		return 0
	}
	if indent("Reviewer") <= indent("Plan") {
		t.Errorf("Reviewer is not nested under Plan:\n%s", out)
	}
	if strings.Contains(out, "Explore") || strings.Contains(out, "Found 3 callers.") {
		t.Errorf("a finished subagent of an unselected session is listed:\n%s", out)
	}
}

func TestSubagentDiffUpdatesTheTree(t *testing.T) {
	st := subagentFixture()
	m := selectSession(t, newModel(&st, nil), "s02")
	stopped := agent("s02", "a2", "Plan", domain.SubagentStopped, "Plan is ready.")
	m = update(m, tui.DiffMsg(rpc.Diff{Subagent: &stopped}))
	if out := screen(m); !strings.Contains(out, "2 subagents done") {
		t.Errorf("stopped subagent not counted:\n%s", out)
	}
	fresh := agent("s02", "a4", "Explore", domain.SubagentRunning, "")
	if out := screen(update(m, tui.DiffMsg(rpc.Diff{Subagent: &fresh}))); !strings.Contains(out, "Explore") {
		t.Errorf("new subagent missing:\n%s", out)
	}
}

func TestSessionsWithoutSubagentsShowNoTree(t *testing.T) {
	st := fixture(3, 1)
	if out := screen(newModel(&st, nil)); strings.Contains(out, "✓") {
		t.Errorf("screen has subagent rows:\n%s", out)
	}
}

func TestCollapsingASessionHidesItsSubagents(t *testing.T) {
	st := subagentFixture()
	m := selectSession(t, newModel(&st, nil), "s02")
	if out := screen(press(m, "o")); strings.Contains(out, "Reviewer") {
		t.Errorf("collapsed session still shows subagents:\n%s", out)
	}
}

func TestLongSubagentListIsCutWithACount(t *testing.T) {
	st := fixture(1, 1)
	for i := range 12 {
		st.Subagents = append(st.Subagents, agent("s01", fmt.Sprint("a", i), fmt.Sprintf("Agent%02d", i), domain.SubagentRunning, ""))
	}
	out := screen(newModel(&st, nil))
	if !strings.Contains(out, "Agent07") || strings.Contains(out, "Agent08") || !strings.Contains(out, "4 more") {
		t.Errorf("list not cut at 8:\n%s", out)
	}
}

func TestSubagentTextCannotRepaintTheTerminal(t *testing.T) {
	st := fixture(1, 1)
	st.Subagents = []domain.Subagent{agent("s01", "a1", "Explore\x1b[2J", domain.SubagentRunning, "done\x1b[31m red")}
	if out := newModel(&st, nil).View().Content; strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x1b[31m red") {
		t.Errorf("escape sequence reached the screen: %q", out)
	}
}
