package tui_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

var keyShiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

func defaultsDialog(t *testing.T) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, Defaults: map[domain.Harness]tui.Defaults{
		domain.HarnessClaude: {Model: "sonnet", Effort: "high"},
		domain.HarnessCodex:  {Model: "gpt-5", Effort: "medium"},
	}})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	return typeText(press(m, "n"), "fix the login bug"), c
}

func startedParams(t *testing.T, m tui.Model, c *fakeCaller) rpc.NewSessionParams {
	t.Helper()
	pressCmd(m, keyEnter)
	if len(c.calls) == 0 {
		t.Fatal("no call")
	}
	return c.calls[0].params.(rpc.NewSessionParams)
}

func TestModelSwitchDialogStartsWithTheHarnessDefaults(t *testing.T) {
	m, c := defaultsDialog(t)
	if out := screen(m); !strings.Contains(out, "sonnet") {
		t.Fatalf("model not prefilled:\n%s", out)
	}
	got := startedParams(t, m, c)
	if got.Harness != "claude" || got.Model != "sonnet" || got.Effort != "high" {
		t.Fatalf("params %+v", got)
	}
}

func TestModelSwitchDialogFollowsTheDefaultsWhenTheHarnessChanges(t *testing.T) {
	m, c := defaultsDialog(t)
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyRight)
	got := startedParams(t, m, c)
	if got.Harness != "codex" || got.Model != "gpt-5" || got.Effort != "medium" {
		t.Fatalf("codex params %+v", got)
	}
	m = pressCmd(m, keyLeft)
	c.calls = nil
	got = startedParams(t, m, c)
	if got.Harness != "claude" || got.Model != "sonnet" || got.Effort != "high" {
		t.Fatalf("back on claude %+v", got)
	}
}

func TestNewSessionDialogReopensOnTheLastCreatedChoices(t *testing.T) {
	st := withWorkspaces(fixture(1, 0))
	st.Sessions = []domain.Session{
		{ID: "old", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "low", StartModel: "sonnet", StartEffort: "low", StartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "new", Harness: domain.HarnessCodex, Model: "gpt-live", Effort: "low", StartModel: "gpt-5", StartEffort: "xhigh", StartWorkspace: "/src/api", StartedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
	}
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, Defaults: map[domain.Harness]tui.Defaults{
		domain.HarnessClaude: {Model: "sonnet", Effort: "high"},
		domain.HarnessCodex:  {Model: "gpt-5-default", Effort: "medium"},
	}})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	m = typeText(press(m, "n"), "fix the login bug")
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyRight)
	m = pressCmd(m, keyEsc)
	m = typeText(press(m, "n"), "fix the login bug")
	if out := screen(m); !strings.Contains(out, "gpt-5") || strings.Contains(out, "gpt-live") || strings.Contains(out, "sonnet") {
		t.Fatalf("dialog did not restore the created choices:\n%s", out)
	}
	got := startedParams(t, m, c)
	want := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "fix the login bug", Harness: "codex", Model: "gpt-5", Effort: "xhigh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params %+v, want %+v", got, want)
	}
}

func TestNewSessionDialogRestoresTheEffortChoicesOfTheLastCreatedHarness(t *testing.T) {
	st := withWorkspaces(fixture(1, 0))
	st.Sessions = []domain.Session{
		{ID: "omp", Harness: domain.HarnessOmp, StartEffort: "off", StartWorkspace: "/src/api", StartedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
	}
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	m = typeText(press(m, "n"), "fix the login bug")
	m = pressCmd(pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyTab), keyTab)
	m = pressCmd(m, keyRight)
	got := startedParams(t, m, c)
	want := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "fix the login bug", Harness: "omp", Effort: "minimal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params %+v, want %+v", got, want)
	}
}

func TestModelSwitchDialogKeepsAModelTheUserPicked(t *testing.T) {
	m, c := defaultsDialog(t)
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyTab)
	m = pressCmd(m, keyRight)
	m = pressCmd(pressCmd(m, keyShiftTab), keyRight)
	got := startedParams(t, m, c)
	want := rpc.NewSessionParams{Workspace: got.Workspace, WorkItem: "fix the login bug", Harness: "codex", Model: "haiku", Effort: "medium"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params %+v, want %+v", got, want)
	}
}
