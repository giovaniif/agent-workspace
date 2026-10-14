package tui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func toolEvent(session, tool, detail string) domain.SessionEvent {
	return domain.SessionEvent{SessionID: session, Kind: domain.EventPreToolUse, Tool: tool, Detail: detail}
}

func cardFixture() rpc.State {
	st := fixture(5, 1)
	for _, cmd := range []string{"go build", "go vet", "go test ./...", "git status"} {
		st.Events = append(st.Events, toolEvent("s05", "Bash", cmd))
	}
	st.Events = append(st.Events, domain.SessionEvent{
		SessionID: "s05", Kind: domain.EventPermissionRequest,
		Text: "Bash: rm -rf build\nmake clean\nmake all\nmake install\nmake deploy",
	})
	return st
}

func selectSession(t *testing.T, m tui.Model, id string) tui.Model {
	t.Helper()
	for i := range 9 {
		if m.Selected() == id {
			return m
		}
		m = press(m, fmt.Sprint(i+1))
	}
	if m.Selected() != id {
		t.Fatalf("could not select %s", id)
	}
	return m
}

func TestTheSidebarHasNoCardPanel(t *testing.T) {
	st := cardFixture()
	st.Worktrees[4].PR.State = domain.PROpen
	m := selectSession(t, newModel(&st, nil), "s05")
	out := screen(m)
	for _, absent := range []string{"CARD", "PRs ", "last  ", "git status", "Bash: rm -rf build", "asks permission"} {
		if strings.Contains(out, absent) {
			t.Errorf("sidebar still shows %q:\n%s", absent, out)
		}
	}
}

func TestTheHelpFooterStaysWithASelectedSession(t *testing.T) {
	st := cardFixture()
	out := screen(selectSession(t, newModel(&st, nil), "s05"))
	lines := strings.Split(out, "\n")
	n := len(lines)
	if !strings.Contains(lines[n-2], "n new  r review  t shell  e nvim  ? keys") {
		t.Errorf("hint line = %q", lines[n-2])
	}
	if !strings.Contains(lines[n-1], "5 sessions") || !strings.Contains(lines[n-1], "next waiting") {
		t.Errorf("status line = %q", lines[n-1])
	}
}

func TestTheSessionListUsesTheRowsTheCardTookUp(t *testing.T) {
	st := fixture(20, 1)
	for _, cmd := range []string{"go build", "go vet", "go test ./..."} {
		st.Events = append(st.Events, toolEvent("s01", "Bash", cmd))
	}
	rows := 0
	for _, l := range strings.Split(screen(newModel(&st, nil)), "\n") {
		if strings.Contains(l, " change") {
			rows++
		}
	}
	if rows < 16 {
		t.Fatalf("%d session rows fit in a 40-row sidebar, want at least 16", rows)
	}
}

func TestTheFooterLeavesTheSessionListItsRows(t *testing.T) {
	st := cardFixture()
	out := screen(selectSession(t, newModel(&st, nil), "s05"))
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, " 5 ") && i+1 < len(lines) && strings.Contains(lines[i+1], "opus-5.5") {
			return
		}
	}
	t.Fatalf("session 5's model line is cut off:\n%s", out)
}
