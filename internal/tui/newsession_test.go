package tui_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

func withWorkspaces(st rpc.State) rpc.State {
	st.Workspaces = []domain.Workspace{
		{Root: "/src/api", Kind: domain.WorkspaceSingle, LastUsed: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{Root: "/src/shop", Kind: domain.WorkspaceOrchestration, LastUsed: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
	}
	return st
}

func run(m tui.Model, cmd tea.Cmd) tui.Model {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(tui.Model)
	}
	return m
}

func typeText(m tui.Model, s string) tui.Model {
	return update(m, tea.PasteMsg{Content: s})
}

func pressCmd(m tui.Model, k tea.KeyPressMsg) tui.Model {
	next, cmd := m.Update(k)
	return run(next.(tui.Model), cmd)
}

var (
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyRight = tea.KeyPressMsg{Code: tea.KeyRight}
	keyLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyBack  = tea.KeyPressMsg{Code: tea.KeyBackspace}
)

func dialogModel(t *testing.T, st rpc.State) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	return press(m, "n"), c
}

func TestNewSessionDialogStartsTheSessionAndShowsItsPane(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(fixture(1, 0)))
	out := screen(m)
	if !strings.Contains(out, "New session") || !strings.Contains(out, "shop") {
		t.Fatalf("dialog does not default to the last used workspace:\n%s", out)
	}
	m = typeText(m, "https://linear.app/acme/issue/ENG-9/add-search")
	if out := screen(m); !strings.Contains(out, "ENG-9 · add search") {
		t.Fatalf("dialog does not say what the work item is:\n%s", out)
	}
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyLeft)
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyRight)
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyRight)
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyRight)
	m = pressCmd(m, keyRight)
	m = pressCmd(m, keyEnter)

	if got := c.methods(); !slices.Equal(got, []string{rpc.MethodNewSession, rpc.MethodSessionFocus}) {
		t.Fatalf("calls %v", got)
	}
	want := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "https://linear.app/acme/issue/ENG-9/add-search", Harness: "codex", Model: "gpt-6.1-sol", Effort: "medium"}
	if !reflect.DeepEqual(c.calls[0].params, want) {
		t.Fatalf("params %+v, want %+v", c.calls[0].params, want)
	}
	if !reflect.DeepEqual(c.calls[1].params, rpc.SessionFocusParams{ID: "new1"}) {
		t.Fatalf("focus %+v", c.calls[1].params)
	}
	if out := screen(m); strings.Contains(out, "New session") {
		t.Fatalf("dialog still open after start:\n%s", out)
	}
}

func TestNewSessionDialogCyclesWorkspacesBothWays(t *testing.T) {
	m, _ := dialogModel(t, withWorkspaces(rpc.State{}))
	m = pressCmd(typeText(m, "x"), keyTab)
	if out := screen(pressCmd(m, keyRight)); !strings.Contains(out, "‹ api ›") {
		t.Fatalf("right from the last workspace should wrap to api:\n%s", out)
	}
	if out := screen(pressCmd(pressCmd(m, keyLeft), keyLeft)); !strings.Contains(out, "‹ shop ›") {
		t.Fatalf("left twice should come back to shop:\n%s", out)
	}
}

func TestNewSessionDialogEscapeCancelsWithoutCalling(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(rpc.State{}))
	m = pressCmd(typeText(m, "fix it"), keyEsc)
	if len(c.methods()) != 0 || strings.Contains(screen(m), "New session") {
		t.Fatalf("calls %v after esc:\n%s", c.methods(), screen(m))
	}
	if m = press(m, "j"); strings.Contains(screen(m), "fix it") {
		t.Fatal("keys after esc still type into the dialog")
	}
}

func TestNewSessionDialogKeepsABrokenURLAsText(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(rpc.State{}))
	m = typeText(m, "https://linear.app/acme/nope")
	if out := screen(m); !strings.Contains(out, "text") {
		t.Fatalf("broken URL not shown as text:\n%s", out)
	}
	pressCmd(m, keyEnter)
	if len(c.calls) == 0 || c.calls[0].params.(rpc.NewSessionParams).WorkItem != "https://linear.app/acme/nope" {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestNewSessionDialogNeedsAWorkItemAndAWorkspace(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(rpc.State{}))
	m = pressCmd(m, keyEnter)
	if len(c.methods()) != 0 || !strings.Contains(screen(m), "work item is empty") {
		t.Fatalf("empty work item: calls %v\n%s", c.methods(), screen(m))
	}
	m, c = dialogModel(t, rpc.State{})
	m = pressCmd(typeText(m, "x"), keyEnter)
	if len(c.methods()) != 0 || !strings.Contains(screen(m), "workspace add") {
		t.Fatalf("no workspace: calls %v\n%s", c.methods(), screen(m))
	}
}

func TestNewSessionFailureKeepsTheDialogAndSaysWhy(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(rpc.State{}))
	c.err = errors.New("failed: branch exists")
	m = pressCmd(typeText(m, "x"), keyEnter)
	if out := screen(m); !strings.Contains(out, "New session") || !strings.Contains(out, "branch exists") {
		t.Fatalf("failure not shown in the dialog:\n%s", out)
	}
}

func TestXEndsTheSelectedSessionAfterYes(t *testing.T) {
	st := fixture(2, 0)
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	m = press(m, "x")
	if !strings.Contains(screen(m), "end session 1?") {
		t.Fatalf("no confirmation:\n%s", screen(m))
	}
	m = pressCmd(m, key("n"))
	if len(c.methods()) != 0 {
		t.Fatalf("n still ended: %v", c.methods())
	}
	m = press(m, "x")
	pressCmd(m, key("y"))
	if !slices.Equal(c.methods(), []string{rpc.MethodEndSession}) || !reflect.DeepEqual(c.calls[0].params, rpc.SessionRef{ID: m.Selected()}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func lowClaudeState() rpc.State {
	st := withWorkspaces(rpc.State{})
	st.Sessions = []domain.Session{
		{ID: "c", Harness: domain.HarnessClaude, LimitsAt: clock(), Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 90}}},
		{ID: "x", Harness: domain.HarnessCodex, LimitsAt: clock(), Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 36}}},
	}
	return st
}

var (
	keyCtrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	keyCtrlN = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	keyCtrlP = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	keyCtrlY = tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	keyCtrlE = tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
)

func TestNewSessionDialogWarnsOnLowQuotaAndSwitchesHarness(t *testing.T) {
	m, c := dialogModel(t, lowClaudeState())
	out := screen(m)
	if !strings.Contains(out, "Claude 5h window 90% used") || !strings.Contains(out, "Codex 5h is 36% used") {
		t.Fatalf("no low-quota warning with a switch:\n%s", out)
	}
	m = pressCmd(m, keyCtrlS)
	if out := screen(m); !strings.Contains(out, "● codex") || strings.Contains(out, "90% used") {
		t.Fatalf("ctrl+s did not switch to codex:\n%s", out)
	}
	pressCmd(typeText(m, "x"), keyEnter)
	if len(c.calls) == 0 || c.calls[0].params.(rpc.NewSessionParams).Harness != "codex" {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestNewSessionDialogWarnsWithoutASwitchWhenTheOtherHarnessIsUnknown(t *testing.T) {
	st := lowClaudeState()
	st.Sessions = st.Sessions[:1]
	m, _ := dialogModel(t, st)
	out := screen(m)
	if !strings.Contains(out, "Claude 5h window 90% used") || strings.Contains(out, "ctrl+s") {
		t.Fatalf("warning should have no switch:\n%s", out)
	}
	if out := screen(pressCmd(m, keyCtrlS)); !strings.Contains(out, "● claude") {
		t.Fatalf("ctrl+s switched with nothing to switch to:\n%s", out)
	}
}

type slowCaller struct {
	fakeCaller
	release chan struct{}
}

func (c *slowCaller) Call(ctx context.Context, method string, params, out any) error {
	if method == rpc.MethodNewSession {
		<-c.release
	}
	return c.fakeCaller.Call(ctx, method, params, out)
}

func TestNewSessionDialogStillQuitsAndClosesWhileStarting(t *testing.T) {
	c := &slowCaller{release: make(chan struct{})}
	defer close(c.release)
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tui.StateMsg(withWorkspaces(rpc.State{})))
	m = typeText(press(m, "n"), "x")
	next, _ := m.Update(keyEnter)
	m = next.(tui.Model)
	if !strings.Contains(screen(m), "starting") {
		t.Fatalf("not starting:\n%s", screen(m))
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c while starting did nothing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c while starting did not quit")
	}
	if out := screen(update(m, keyEsc)); strings.Contains(out, "New session") {
		t.Fatalf("esc while starting did not close the dialog:\n%s", out)
	}
}

func TestEndConfirmationEndsTheSessionItWasAskedAbout(t *testing.T) {
	st := withWorkspaces(fixture(2, 0))
	m, c := dialogModel(t, st)
	m = pressCmd(typeText(m, "x"), keyEnter)
	asked := m.Selected()
	m = press(m, "x")
	started := domain.Session{ID: "new1", Pane: "%5", State: domain.StateIdle}
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 1, Session: &started}))
	if m.Selected() != "new1" {
		t.Fatalf("the late diff did not select the new session: %q", m.Selected())
	}
	pressCmd(m, key("y"))
	last := c.calls[len(c.calls)-1]
	if last.method != rpc.MethodEndSession || !reflect.DeepEqual(last.params, rpc.SessionRef{ID: asked}) {
		t.Fatalf("ended %+v, want %s", last, asked)
	}
}

func TestNewSessionDialogScrollsToTheActiveFieldOnAShortTerminal(t *testing.T) {
	m, _ := dialogModel(t, withWorkspaces(rpc.State{}))
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 16})
	for range 4 {
		m = pressCmd(m, keyTab)
	}
	if out := screen(m); !strings.Contains(out, "Effort") {
		t.Fatalf("the active Effort row is off screen:\n%s", out)
	}
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyEnter)
	if out := screen(m); !strings.Contains(out, "work item is empty") {
		t.Fatalf("the error is off screen:\n%s", out)
	}
}

func TestAStartReplyOnlyTouchesTheDialogThatSentIt(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tui.StateMsg(withWorkspaces(rpc.State{})))
	m = typeText(press(m, "n"), "first")
	next, startA := m.Update(keyEnter)
	m = update(next.(tui.Model), keyEsc)
	m = typeText(press(m, "n"), "second")
	m = run(m, startA)
	if out := screen(m); !strings.Contains(out, "New session") || !strings.Contains(out, "second") {
		t.Fatalf("the first start's reply closed the second dialog:\n%s", out)
	}
	c.err = errors.New("failed: boom")
	m = typeText(press(update(m, keyEsc), "n"), "third")
	next, startB := m.Update(keyEnter)
	m = update(next.(tui.Model), keyEsc)
	m = typeText(press(m, "n"), "fourth")
	m = run(m, startB)
	if out := screen(m); strings.Contains(out, "✗") || strings.Contains(out, "starting") || !strings.Contains(out, "fourth") {
		t.Fatalf("the third start's failure landed on the fourth dialog:\n%s", out)
	}
}

func TestEndedAndRemovedSessionsLeaveTheSidebar(t *testing.T) {
	st := fixture(3, 0)
	st.Sessions[2].Ended = true
	m := newModel(&st, nil)
	if out := screen(m); !strings.Contains(out, "2 sessions") {
		t.Fatalf("the ended session is still counted:\n%s", out)
	}
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 1, RemovedSession: "s02"}))
	if out := screen(m); !strings.Contains(out, "1 session ") {
		t.Fatalf("the removed session is still listed:\n%s", out)
	}
}

func threeIdle() rpc.State {
	return rpc.State{
		Tasks: []domain.Task{{ID: "t"}},
		Sessions: []domain.Session{
			{ID: "a", TaskID: "t", State: domain.StateIdle},
			{ID: "b", TaskID: "t", State: domain.StateIdle},
			{ID: "c", TaskID: "t", State: domain.StateIdle},
		},
	}
}

func TestTheRowAfterAnEndedOrRemovedSessionTakesTheSelection(t *testing.T) {
	pick := func(keys ...string) tui.Model {
		st := threeIdle()
		return press(newModel(&st, nil), keys...)
	}
	m := pick("j")
	if m.Selected() != "b" {
		t.Fatalf("selected %q; want b", m.Selected())
	}
	if got := update(pick("j"), tui.DiffMsg(rpc.Diff{Seq: 1, RemovedSession: "b"})).Selected(); got != "c" {
		t.Fatalf("after b was removed selected %q; want c", got)
	}
	ended := threeIdle().Sessions[1]
	ended.Ended = true
	if got := update(pick("j"), tui.DiffMsg(rpc.Diff{Seq: 1, Session: &ended})).Selected(); got != "c" {
		t.Fatalf("after b ended selected %q; want c", got)
	}
	if got := update(pick("j", "j"), tui.DiffMsg(rpc.Diff{Seq: 1, RemovedSession: "c"})).Selected(); got != "b" {
		t.Fatalf("after the last row went selected %q; want b", got)
	}
}

func TestTheSelectionFollowsASessionFocusedFromOutside(t *testing.T) {
	st := threeIdle()
	m := newModel(&st, nil)
	if m.Selected() != "a" {
		t.Fatalf("selected %q; want a", m.Selected())
	}
	focused := threeIdle().Sessions[2]
	focused.Focused = true
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 1, Session: &focused}))
	if m.Selected() != "c" {
		t.Fatalf("after c was focused selected %q; want c", m.Selected())
	}
	m = press(m, "k")
	changed := focused
	changed.State = domain.StateDone
	if got := update(m, tui.DiffMsg(rpc.Diff{Seq: 2, Session: &changed})).Selected(); got != "b" {
		t.Fatalf("a change to the already focused c moved the selection to %q; want b", got)
	}
}
