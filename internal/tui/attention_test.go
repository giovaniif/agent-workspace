package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func newAttendModel(st *rpc.State, a tui.Attender) tui.Model {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Focus: &fakeFocuser{}, Attend: a})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(*st))
}

func TestNotifyMKeyTogglesMuteOnTheSelectedSession(t *testing.T) {
	st := fixture(2, 0)
	a := &fakeAttender{}
	m := newAttendModel(&st, a)
	_, cmd := m.Update(key("m"))
	if cmd == nil {
		t.Fatal("m returned no command")
	}
	cmd()
	if len(a.muted) != 1 || a.muted[0] != (mute{"s01", true}) {
		t.Fatalf("mute calls %+v", a.muted)
	}

	st.Sessions[0].Muted = true
	m = update(m, tui.StateMsg(st))
	_, cmd = m.Update(key("m"))
	cmd()
	if len(a.muted) != 2 || a.muted[1] != (mute{"s01", false}) {
		t.Fatalf("unmute calls %+v", a.muted)
	}
}

func TestNotifyMutedSessionShowsTheMuteMarker(t *testing.T) {
	st := fixture(2, 0)
	m := newAttendModel(&st, &fakeAttender{})
	if strings.Contains(screen(m), "⊘") {
		t.Fatalf("marker without mute:\n%s", screen(m))
	}
	st.Sessions[1].Muted = true
	m = update(m, tui.StateMsg(st))
	if strings.Count(screen(m), "⊘") != 1 {
		t.Fatalf("want one marker:\n%s", screen(m))
	}
}

func TestNotifyEnterMarksTheSessionSeen(t *testing.T) {
	st := fixture(2, 0)
	a := &fakeAttender{}
	m := newAttendModel(&st, a)
	m = press(m, "j")
	_, cmd := m.Update(key("enter"))
	cmd()
	if len(a.focused) != 1 || a.focused[0] != "s02" {
		t.Fatalf("focus calls %v", a.focused)
	}
}

func TestNotifyHelpListsMute(t *testing.T) {
	st := fixture(1, 0)
	if out := screen(press(newModel(&st, nil), "?")); !strings.Contains(out, "mute session") {
		t.Fatalf("help missing mute:\n%s", out)
	}
}
