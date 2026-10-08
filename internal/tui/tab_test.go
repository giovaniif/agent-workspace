package tui_test

import (
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

const tabWT = "/h/api/x"

func tabsState() rpc.State {
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	return rpc.State{
		Projects: []domain.Project{{Root: "/src/shop", Name: "shop"}},
		Tasks: []domain.Task{
			{ID: "t1", Text: "checkout flow"},
			{ID: "t2", Text: "solo thing"},
		},
		Worktrees: []domain.Worktree{
			{ID: tabWT, Repo: "/src/shop/api", Path: tabWT, Branch: "x", SessionID: "s01"},
			{ID: "/h/solo/y", Repo: "/src/solo", Path: "/h/solo/y", SessionID: "s02"},
		},
		Sessions: []domain.Session{
			{ID: "s01", TaskID: "t1", Harness: domain.HarnessClaude, Pane: "%1", WorktreeIDs: []string{tabWT}, StartedAt: t0},
			{ID: "s02", TaskID: "t2", Harness: domain.HarnessClaude, Pane: "%2", WorktreeIDs: []string{"/h/solo/y"}, StartedAt: t0},
			{ID: "s03", TaskID: "t1", Harness: domain.HarnessCodex, Pane: "%3", Tab: tabWT, State: domain.StatePermission, StartedAt: t0.Add(2 * time.Minute)},
		},
		ShellTabs:  []domain.ShellTab{{ID: "sh1", Worktree: tabWT, Pane: "%4", At: t0.Add(time.Minute)}},
		ActiveTabs: map[string]string{tabWT: "s01"},
	}
}

func tabsModel(t *testing.T, selected string) (tui.Model, *fakeCaller) {
	t.Helper()
	return tabsModelWith(t, selected, tui.Options{})
}

func tabsModelWith(t *testing.T, selected string, opts tui.Options) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	opts.Theme, opts.Now, opts.Calls = tui.Latte(), clock, c
	m := tui.New(opts)
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(tabsState()))
	for range 5 {
		if m.Selected() == selected {
			break
		}
		m = press(m, "j")
	}
	if m.Selected() != selected {
		t.Fatalf("could not select %s (at %s)", selected, m.Selected())
	}
	return m, c
}

func TestTabLineListsTheProjectWorktreesTabsUnderTheSelectedRow(t *testing.T) {
	m, _ := tabsModel(t, "s01")
	out := screen(m)
	if !strings.Contains(out, "[1 ○ claude]  2 shell  3 ✳ codex") {
		t.Fatalf("no tab line:\n%s", out)
	}
	m, _ = tabsModel(t, "s03")
	if !strings.Contains(screen(m), "[1 ○ claude]  2 shell  3 ✳ codex") {
		t.Fatalf("an agent tab's row shows no tab line:\n%s", screen(m))
	}
}

func TestTabLineFollowsShellTabAndActiveTabDiffs(t *testing.T) {
	m, _ := tabsModel(t, "s01")
	m = update(m, tui.DiffMsg(rpc.Diff{ShellTab: &domain.ShellTab{ID: "sh2", Worktree: tabWT, Pane: "%5", At: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)}}))
	m = update(m, tui.DiffMsg(rpc.Diff{ActiveTab: &rpc.ActiveTab{Worktree: tabWT, Tab: "sh2"}}))
	m = update(m, tui.DiffMsg(rpc.Diff{RemovedShellTab: "sh1"}))
	if out := screen(m); !strings.Contains(out, "1 ○ claude  2 ✳ codex  [3 shell]") {
		t.Fatalf("tab line not updated:\n%s", out)
	}
}

func TestTabKeysDoNothingOutsideProjects(t *testing.T) {
	m, c := tabsModel(t, "s02")
	if strings.Contains(screen(m), "2 shell") {
		t.Fatalf("a session outside projects shows tabs:\n%s", screen(m))
	}
	for _, k := range []string{"[", "]", "+", "-"} {
		m = pressCmd(m, key(k))
	}
	if len(c.methods()) != 0 || strings.Contains(screen(m), "NEW TAB") {
		t.Fatalf("tab keys acted outside projects: %v\n%s", c.methods(), screen(m))
	}
}

func TestTabBracketsStepThroughTheTabs(t *testing.T) {
	m, c := tabsModel(t, "s01")
	m = pressCmd(m, key("]"))
	pressCmd(m, key("["))
	want := []call{
		{rpc.MethodTabStep, rpc.TabParams{Session: "s01", Delta: 1}},
		{rpc.MethodTabStep, rpc.TabParams{Session: "s01", Delta: -1}},
	}
	if !reflect.DeepEqual(c.calls, want) {
		t.Fatalf("calls %+v, want %+v", c.calls, want)
	}
}

func TestTabClickOnTheTabLineShowsThatTab(t *testing.T) {
	m, c := tabsModel(t, "s01")
	clickOn(t, m, "2 shell")
	if !slices.Equal(c.methods(), []string{rpc.MethodTabShow}) || !reflect.DeepEqual(c.calls[0].params, rpc.TabParams{Session: "s01", Tab: "sh1"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	c.calls = nil
	clickOn(t, m, "3 ✳ codex")
	if len(c.calls) != 1 || !reflect.DeepEqual(c.calls[0].params, rpc.TabParams{Session: "s01", Tab: "s03"}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestTabCloseAsksThenClosesTheShownTab(t *testing.T) {
	m, c := tabsModel(t, "s01")
	m = pressCmd(m, key("-"))
	if !strings.Contains(screen(m), "close tab 1 claude? y/n") || len(c.calls) != 0 {
		t.Fatalf("- did not ask first (%v):\n%s", c.methods(), screen(m))
	}
	pressCmd(m, key("y"))
	if !slices.Equal(c.methods(), []string{rpc.MethodTabClose}) || !reflect.DeepEqual(c.calls[0].params, rpc.TabParams{Session: "s01", Tab: "s01"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	m, c = tabsModel(t, "s01")
	m = pressCmd(m, key("-"))
	pressCmd(m, key("n"))
	if len(c.calls) != 0 {
		t.Fatalf("n closed the tab: %+v", c.calls)
	}
}

func TestTabCloseWithoutAKnownActiveTabClosesTheSelectedAgentsTab(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	st := tabsState()
	st.ActiveTabs = nil
	m = update(m, tui.StateMsg(st))
	if m.Selected() != "s03" {
		t.Fatalf("selected %s, want s03 first", m.Selected())
	}
	m = pressCmd(m, key("-"))
	if !strings.Contains(screen(m), "close tab 3 codex? y/n") {
		t.Fatalf("- asked about another tab:\n%s", screen(m))
	}
	pressCmd(m, key("y"))
	if len(c.calls) != 1 || !reflect.DeepEqual(c.calls[0].params, rpc.TabParams{Session: "s03", Tab: "s03"}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestTabNewOpensAShell(t *testing.T) {
	m, c := tabsModel(t, "s01")
	m = pressCmd(m, key("+"))
	if out := screen(m); !strings.Contains(out, "NEW TAB") || !strings.Contains(out, "shell") || !strings.Contains(out, "codex") {
		t.Fatalf("no tab chooser:\n%s", out)
	}
	m = pressCmd(m, keyEnter)
	if !slices.Equal(c.methods(), []string{rpc.MethodTabNew}) || !reflect.DeepEqual(c.calls[0].params, rpc.TabParams{Session: "s01", Kind: "shell"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	if strings.Contains(screen(m), "NEW TAB") {
		t.Fatalf("the chooser stayed open:\n%s", screen(m))
	}
}

func TestTabNewOpensAnAgentWithTheChosenModelAndEffort(t *testing.T) {
	m, c := tabsModelWith(t, "s01", tui.Options{Defaults: map[domain.Harness]tui.Defaults{domain.HarnessCodex: {Model: "gpt-5.5", Effort: "high"}}})
	m = pressCmd(m, key("+"))
	m = press(m, "j", "j")
	if !strings.Contains(screen(m), "gpt-5.5") {
		t.Fatalf("codex row lacks its default model:\n%s", screen(m))
	}
	m = pressCmd(m, keyEnter)
	want := rpc.TabParams{Session: "s01", Kind: "agent", Harness: "codex", Model: "gpt-5.5", Effort: "high"}
	if len(c.calls) != 1 || !reflect.DeepEqual(c.calls[0].params, want) {
		t.Fatalf("calls %+v, want %+v", c.calls, want)
	}

	c.calls = nil
	m = pressCmd(m, key("+"))
	m = press(m, "j", "j")
	m = pressCmd(m, keyRight)
	m = pressCmd(m, keyTab)
	m = pressCmd(m, keyRight)
	pressCmd(m, keyEnter)
	if len(c.calls) != 1 {
		t.Fatalf("calls %+v", c.calls)
	}
	got := c.calls[0].params.(rpc.TabParams)
	if got.Harness != "codex" || got.Model == "gpt-5.5" || got.Effort == "high" {
		t.Fatalf("→ and tab did not change the model and effort: %+v", got)
	}
}

func TestTabNewChooserClosesOnEsc(t *testing.T) {
	m, c := tabsModel(t, "s01")
	m = pressCmd(m, key("+"))
	m = pressCmd(m, keyEsc)
	if strings.Contains(screen(m), "NEW TAB") || len(c.calls) != 0 {
		t.Fatalf("esc did not close the chooser (%v):\n%s", c.methods(), screen(m))
	}
}

func TestTabEntriesInTheContextMenuOnlyForProjectSessions(t *testing.T) {
	m, c := tabsModel(t, "s01")
	m = rightClickOn(t, m, "2 checkout flow")
	if !strings.Contains(screen(m), "Close tab") || !strings.Contains(screen(m), "New tab") {
		t.Fatalf("menu lacks the tab entries:\n%s", screen(m))
	}
	m = clickOn(t, m, "Close tab")
	if !strings.Contains(screen(m), "close tab 1 claude? y/n") {
		t.Fatalf("Close tab did not ask:\n%s", screen(m))
	}
	pressCmd(m, key("y"))
	if !slices.Equal(c.methods(), []string{rpc.MethodTabClose}) {
		t.Fatalf("calls %+v", c.calls)
	}
	m, _ = tabsModel(t, "s02")
	m = rightClickOn(t, m, "3 solo thing")
	if out := screen(m); strings.Contains(out, "Close tab") || strings.Contains(out, "New tab") || !strings.Contains(out, "End session") {
		t.Fatalf("a session outside projects got tab entries:\n%s", out)
	}
}
