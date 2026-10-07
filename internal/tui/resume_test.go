package tui_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

func resumeModel(t *testing.T, sessions ...domain.Session) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	st := rpc.State{
		Tasks: []domain.Task{
			{ID: "t1", Source: domain.TaskText, Text: "fix login"},
			{ID: "t2", Source: domain.TaskText, Text: "retry queue"},
		},
		Sessions: sessions,
	}
	return update(m, tui.StateMsg(st)), c
}

func TestResumeUOpensTheEndedSessionsAndEnterResumesAndShowsOne(t *testing.T) {
	m, c := resumeModel(t,
		domain.Session{ID: "live", TaskID: "t1", Harness: domain.HarnessClaude, Pane: "%1", ResumeID: "r0", Dir: "/w/a"},
		domain.Session{ID: "gone", TaskID: "t2", Harness: domain.HarnessCodex, Ended: true, ResumeID: "r1", Dir: "/w/retry"},
		domain.Session{ID: "lost", TaskID: "t1", Harness: domain.HarnessClaude, Ended: true, Dir: "/w/b"},
	)
	m = press(m, "u")
	out := screen(m)
	if !strings.Contains(out, "RESUME") || !strings.Contains(out, "1  retry queue") || strings.Contains(out, "2  ") {
		t.Fatalf("picker:\n%s", out)
	}
	m = pressCmd(m, key("enter"))
	if !slices.Equal(c.methods(), []string{rpc.MethodResumeSession, rpc.MethodSessionFocus}) ||
		!reflect.DeepEqual(c.calls[0].params, rpc.SessionRef{ID: "gone"}) ||
		!reflect.DeepEqual(c.calls[1].params, rpc.SessionFocusParams{ID: "gone"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	if strings.Contains(screen(m), "RESUME") {
		t.Fatalf("picker still open:\n%s", screen(m))
	}
}

func TestResumeEscClosesThePickerWithoutResuming(t *testing.T) {
	m, c := resumeModel(t, domain.Session{ID: "gone", TaskID: "t2", Ended: true, ResumeID: "r1", Dir: "/w"})
	m = pressCmd(press(m, "u"), keyEsc)
	if len(c.methods()) != 0 || strings.Contains(screen(m), "RESUME") {
		t.Fatalf("calls %v, screen:\n%s", c.methods(), screen(m))
	}
}

func TestResumeWithNothingToResumeSaysSo(t *testing.T) {
	m, _ := resumeModel(t, domain.Session{ID: "live", TaskID: "t1", Pane: "%1"})
	if out := screen(press(m, "u")); !strings.Contains(out, "no ended session to resume") {
		t.Fatalf("screen:\n%s", out)
	}
}

func TestResumePickerScrollsToTheChosenRow(t *testing.T) {
	var sessions []domain.Session
	for i := range 30 {
		sessions = append(sessions, domain.Session{ID: fmt.Sprintf("s%02d", i), TaskID: "t2", Ended: true, ResumeID: "r", Dir: fmt.Sprintf("/w/dir%02d", i)})
	}
	m, _ := resumeModel(t, sessions...)
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 12})
	m = press(m, "u")
	for range 29 {
		m = press(m, "j")
	}
	if out := screen(m); !strings.Contains(out, "▌30  retry queue") {
		t.Fatalf("chosen row not in view:\n%s", out)
	}
}

func TestResumeMouseClickOnAChoiceResumesAndFocusesIt(t *testing.T) {
	m, c := resumeModel(t,
		domain.Session{ID: "a", TaskID: "t1", Harness: domain.HarnessClaude, Ended: true, ResumeID: "r1", Dir: "/w/a"},
		domain.Session{ID: "b", TaskID: "t2", Harness: domain.HarnessCodex, Ended: true, ResumeID: "r2", Dir: "/w/b"},
	)
	m = press(m, "u")
	x, y := spot(t, m, "2  ")
	line := strings.Split(screen(m), "\n")[y]
	m = drive(m, click(x, y))
	if len(c.calls) != 2 || c.calls[0].method != rpc.MethodResumeSession {
		t.Fatalf("calls %+v", c.calls)
	}
	id := c.calls[0].params.(rpc.SessionRef).ID
	if !strings.Contains(line, "/"+id) && !strings.Contains(line, " "+id) {
		t.Fatalf("clicked %q but resumed %s", line, id)
	}
	if !reflect.DeepEqual(c.calls[1].params, rpc.SessionFocusParams{ID: id}) {
		t.Fatalf("calls %+v", c.calls)
	}
	if strings.Contains(screen(m), "RESUME") {
		t.Fatalf("picker still open:\n%s", screen(m))
	}
}

func TestResumeMouseClickOnTitleBlankOrFooterTextDoesNothing(t *testing.T) {
	m, c := resumeModel(t, domain.Session{ID: "gone", TaskID: "t2", Ended: true, ResumeID: "r1", Dir: "/w"})
	m = press(m, "u")
	tx, ty := spot(t, m, "RESUME")
	fx, fy := spot(t, m, "pick")
	for _, at := range [][2]int{{tx, ty}, {2, ty - 1}, {fx, fy}} {
		m = drive(m, click(at[0], at[1]))
	}
	if len(c.methods()) != 0 || !strings.Contains(screen(m), "RESUME") {
		t.Fatalf("calls %v, screen:\n%s", c.methods(), screen(m))
	}
}

func TestResumeMouseClickOnAScrolledPickerResumesTheRowUnderThePointer(t *testing.T) {
	var sessions []domain.Session
	for i := range 30 {
		sessions = append(sessions, domain.Session{ID: fmt.Sprintf("s%02d", i), TaskID: "t2", Ended: true, ResumeID: "r", Dir: fmt.Sprintf("/w/dir%02d", i)})
	}
	m, c := resumeModel(t, sessions...)
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 12})
	m = press(m, "u")
	for range 29 {
		m = press(m, "j")
	}
	x, y := spot(t, m, "28  ")
	line := strings.Split(screen(m), "\n")[y]
	drive(m, click(x, y))
	if len(c.calls) == 0 || c.calls[0].method != rpc.MethodResumeSession {
		t.Fatalf("calls %+v", c.calls)
	}
	id := c.calls[0].params.(rpc.SessionRef).ID
	if !strings.Contains(line, "dir"+id[1:]) {
		t.Fatalf("clicked %q but resumed %s", line, id)
	}
}
