package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/tui"
)

func spot(t testing.TB, m tui.Model, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(screen(m), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, screen(m))
	return 0, 0
}

func click(x, y int) tea.Msg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func release(x, y int) tea.Msg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func clickOn(t testing.TB, m tui.Model, text string) tui.Model {
	t.Helper()
	x, y := spot(t, m, text)
	return drive(m, click(x, y), release(x, y))
}

func wheel(m tui.Model, down bool, n int) tui.Model {
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	for range n {
		m = update(m, tea.MouseWheelMsg{X: 2, Y: 8, Button: b})
	}
	return m
}

func TestMouseClickOnACardSelectsThatSession(t *testing.T) {
	st := fixture(4, 1)
	m := newModel(&st, nil)
	m = clickOn(t, m, "task number 2")
	if m.Selected() != "s03" {
		t.Fatalf("clicking the second task's header row selected %q, want s03", m.Selected())
	}
	if m = clickOn(t, m, "session 2 change"); m.Selected() != "s02" {
		t.Fatalf("clicking the second card's row selected %q, want s02", m.Selected())
	}
}

func TestMouseClickOnACardShowsAndFocusesThatSessionAtOnce(t *testing.T) {
	st := fixture(3, 1)
	a := &fakeAttender{}
	m := newAttendModel(&st, a)
	m = clickOn(t, m, "task number 2")
	if m.Selected() != "s03" {
		t.Fatalf("one click on the second task selected %q, want s03", m.Selected())
	}
	if len(a.focused) != 1 || a.focused[0] != "s03" {
		t.Fatalf("one click focused %v, want [s03]", a.focused)
	}
	clickOn(t, m, "task number 2")
	if len(a.focused) != 2 || a.focused[1] != "s03" {
		t.Fatalf("a click on the selected card focused %v, want it focused again", a.focused)
	}
}

func TestMouseClickOnAFooterHintFiresItsKey(t *testing.T) {
	st := fixture(2, 1)
	m := newModel(&st, nil)
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 60})
	m = clickOn(t, m, "? keys")
	if !strings.Contains(screen(m), "KEYS") {
		t.Fatalf("clicking the ? hint did not open help:\n%s", screen(m))
	}
	m = clickOn(t, m, "close help")
	if strings.Contains(screen(m), "close help") {
		t.Fatalf("clicking the ? row in help did not close it:\n%s", screen(m))
	}
}

func TestMousePickerClickAppliesThatChoice(t *testing.T) {
	st := fixture(1, 0)
	sw := &fakeSwitcher{}
	m := switchModel(&st, sw)
	m = press(m, "E")
	clickOn(t, m, "3  ")
	if len(sw.calls) != 1 || !strings.HasSuffix(sw.calls[0], domain.SwitchChoices(domain.HarnessClaude, domain.SwitchEffort)[2]) {
		t.Fatalf("clicking the third choice switched %v", sw.calls)
	}
}

func TestMouseClicksNeverTypeIntoAFocusedField(t *testing.T) {
	m, _ := dialogModel(t, withWorkspaces(fixture(1, 0)))
	x, y := spot(t, m, "n new")
	m = drive(m, click(x+1, y), release(x+1, y))
	if strings.Contains(screen(m), "n▏") {
		t.Fatalf("a footer click typed into the work item field:\n%s", screen(m))
	}
	m = clickOn(t, m, "esc cancel")
	if strings.Contains(screen(m), "New session") {
		t.Fatalf("clicking esc cancel left the dialog open:\n%s", screen(m))
	}
}

func TestMouseDialogClickOnAFieldFocusesIt(t *testing.T) {
	m, _ := dialogModel(t, withWorkspaces(fixture(1, 0)))
	m = clickOn(t, m, "‹ shop ›")
	m = update(m, keyRight)
	if !strings.Contains(screen(m), "‹ api ›") {
		t.Fatalf("after clicking the workspace field, → did not change it:\n%s", screen(m))
	}
}

func TestMouseWorktreesClickSelectsTheRow(t *testing.T) {
	r := diskModel()
	m := drive(r.m, key("w"))
	m = clickOn(t, m, "feat-c")
	m = drive(m, key("g"))
	if m.Selected() != "s02" {
		t.Fatalf("clicking feat-c then g went to %q, want s02", m.Selected())
	}
}

func TestMouseReviewClickOpensAFileAndPlacesTheCursor(t *testing.T) {
	m, rv := reviewModel(t, 160, 40)
	m = drive(m, key("r"))
	m = clickOn(t, m, "ShareSheet.tsx")
	if !strings.Contains(screen(m), "src/ShareSheet.tsx") {
		t.Fatalf("clicking the file did not open it:\n%s", screen(m))
	}
	m = clickOn(t, m, "export default ShareSheet")
	m = drive(m, key("c"))
	m = drive(m, key("x"))
	drive(m, keyEnter)
	if len(rv.comments) != 1 || rv.comments[0].StartLine != 2 || rv.comments[0].EndLine != 2 {
		t.Fatalf("comment after clicking line 2 = %+v", rv.comments)
	}
}

func TestMouseReviewDragSelectsARange(t *testing.T) {
	m, rv := reviewModel(t, 160, 40)
	m = drive(m, key("r"))
	x1, y1 := spot(t, m, "const token")
	_, y2 := spot(t, m, "if (!token)")
	m = drive(m, click(x1, y1), tea.MouseMotionMsg{X: x1, Y: y2, Button: tea.MouseLeft}, release(x1, y2))
	m = drive(m, key("c"))
	m = drive(m, key("x"))
	drive(m, keyEnter)
	if len(rv.comments) != 1 || rv.comments[0].EndLine != rv.comments[0].StartLine+1 {
		t.Fatalf("dragging over two lines commented %+v", rv.comments)
	}
}

func TestMouseReviewChipSwitchesScope(t *testing.T) {
	m, rv := reviewModel(t, 160, 40)
	m = drive(m, key("r"))
	clickOn(t, m, "last turn")
	if got := rv.asked[len(rv.asked)-1].Scope; got != domain.ScopeLastTurn {
		t.Fatalf("clicking the last turn chip asked for %q", got)
	}
}

func TestMouseOffIgnoresClicksAndAsksForNoEvents(t *testing.T) {
	st := fixture(3, 1)
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, NoMouse: true})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatalf("mouse off still asks for mouse events")
	}
	x, y := spot(t, m, "session 2 change")
	if m = drive(m, click(x, y), release(x, y)); m.Selected() != "s01" {
		t.Fatalf("mouse off still took a click")
	}
	on := newModel(&st, nil)
	if on.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse on does not ask for clicks and drags")
	}
}

func TestMouseSettingReadsUIMouse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if on, err := tui.LoadMouse(path); err != nil || !on {
		t.Fatalf("missing config: mouse %v, %v; want on", on, err)
	}
	if err := os.WriteFile(path, []byte("[ui]\nmouse = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if on, err := tui.LoadMouse(path); err != nil || on {
		t.Fatalf("mouse = false: mouse %v, %v; want off", on, err)
	}
}

func BenchmarkMouseClick(b *testing.B) {
	st := fixture(10, 3)
	m := newModel(&st, nil)
	_, y := spot(b, m, "session 5 change")
	b.ResetTimer()
	for b.Loop() {
		update(m, click(3, y))
	}
}
