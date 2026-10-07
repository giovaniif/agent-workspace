package tui_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

func rightClick(x, y int) tea.Msg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight}
}

func menuModel(t *testing.T) (tui.Model, *fakeCaller) {
	t.Helper()
	st := fixture(4, 1)
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(st)), c
}

func rightClickOn(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	x, y := spot(t, m, text)
	return drive(m, rightClick(x, y))
}

func TestContextMenuOpensUnderTheRightClickedSessionRowAndSelectsIt(t *testing.T) {
	m, _ := menuModel(t)
	_, row := spot(t, m, "session 2 change")
	m = rightClickOn(t, m, "session 2 change")
	if m.Selected() != "s02" {
		t.Fatalf("right-click selected %q, want s02", m.Selected())
	}
	_, at := spot(t, m, "End session")
	if at <= row || at > row+3 {
		t.Fatalf("menu at row %d, want just under row %d:\n%s", at, row, screen(m))
	}
}

func TestContextMenuEndSessionByClickAsksThenEndsLikeX(t *testing.T) {
	m, c := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	m = clickOn(t, m, "End session")
	if strings.Contains(screen(m), "End session") || !strings.Contains(screen(m), "end session 2? y/n") {
		t.Fatalf("choosing End session did not ask:\n%s", screen(m))
	}
	if len(c.methods()) != 0 {
		t.Fatalf("ended before confirming: %v", c.methods())
	}
	pressCmd(m, key("y"))
	if !slices.Equal(c.methods(), []string{rpc.MethodEndSession}) || !reflect.DeepEqual(c.calls[0].params, rpc.SessionRef{ID: "s02"}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestContextMenuEndSessionByKeyboard(t *testing.T) {
	m, c := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	m = pressCmd(m, key("down"))
	m = pressCmd(m, key("up"))
	m = pressCmd(m, keyEnter)
	if !strings.Contains(screen(m), "end session 2? y/n") {
		t.Fatalf("enter on End session did not ask:\n%s", screen(m))
	}
	pressCmd(m, key("y"))
	if !slices.Equal(c.methods(), []string{rpc.MethodEndSession}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestContextMenuArrowsDoNotMoveTheSidebarSelection(t *testing.T) {
	m, _ := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	m = pressCmd(m, key("down"))
	if m.Selected() != "s02" || !strings.Contains(screen(m), "End session") {
		t.Fatalf("down moved the sidebar to %q or closed the menu:\n%s", m.Selected(), screen(m))
	}
}

func TestContextMenuClosesOnEscWithoutEnding(t *testing.T) {
	m, c := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	m = pressCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(screen(m), "End session") || strings.Contains(screen(m), "y/n") || len(c.methods()) != 0 {
		t.Fatalf("esc left the menu or a prompt, calls %v:\n%s", c.methods(), screen(m))
	}
}

func TestContextMenuClosesOnAClickOutsideWithoutActing(t *testing.T) {
	m, c := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	m = clickOn(t, m, "SESSIONS")
	if strings.Contains(screen(m), "End session") || len(c.methods()) != 0 {
		t.Fatalf("click outside left the menu, calls %v:\n%s", c.methods(), screen(m))
	}
	if m = clickOn(t, m, "session 4 change"); m.Selected() != "s04" {
		t.Fatalf("left-click after the menu closed selected %q, want s04", m.Selected())
	}
}

func TestContextMenuClosesWhenTheSelectionChanges(t *testing.T) {
	m, _ := menuModel(t)
	m = rightClickOn(t, m, "session 2 change")
	st := fixture(4, 1)
	st.Sessions = slices.DeleteFunc(st.Sessions, func(s domain.Session) bool { return s.ID == "s02" })
	m = update(m, tui.StateMsg(st))
	if strings.Contains(screen(m), "End session") {
		t.Fatalf("menu stayed after its session left:\n%s", screen(m))
	}
}

func TestContextMenuIgnoresRightClicksOffSessionRows(t *testing.T) {
	m, _ := menuModel(t)
	for _, text := range []string{"task number 2", "SESSIONS"} {
		before := m.Selected()
		m = rightClickOn(t, m, text)
		if strings.Contains(screen(m), "End session") || m.Selected() != before {
			t.Fatalf("right-click on %q opened a menu or moved to %q:\n%s", text, m.Selected(), screen(m))
		}
	}
	m = drive(m, rightClick(2, 38))
	if strings.Contains(screen(m), "End session") {
		t.Fatalf("right-click on empty space opened a menu:\n%s", screen(m))
	}
}
