package domain

import (
	"reflect"
	"testing"
	"time"
)

func tabIDs(tabs []Tab) []string {
	ids := make([]string, 0, len(tabs))
	for _, t := range tabs {
		ids = append(ids, t.ID)
	}
	return ids
}

func TestTabHomeIsTheProjectWorktreeASessionWorksIn(t *testing.T) {
	projects := []Project{{Root: "/code/shop", Name: "shop"}}
	worktrees := []Worktree{
		{ID: "/h/solo/a", Repo: "/code/solo", Path: "/h/solo/a"},
		{ID: "/h/api/b", Repo: "/code/shop/api", Path: "/h/api/b"},
	}
	cases := []struct {
		name    string
		session Session
		want    string
		ok      bool
	}{
		{"a project worktree it holds", Session{WorktreeIDs: []string{"/h/solo/a", "/h/api/b"}}, "/h/api/b", true},
		{"only worktrees outside projects", Session{WorktreeIDs: []string{"/h/solo/a"}}, "", false},
		{"no worktree at all", Session{}, "", false},
		{"a tab of a project worktree", Session{Tab: "/h/api/b"}, "/h/api/b", true},
		{"a tab whose worktree is gone", Session{Tab: "/h/gone"}, "", false},
		{"a worktree id it holds that is unknown", Session{WorktreeIDs: []string{"/h/gone"}}, "", false},
	}
	for _, c := range cases {
		got, ok := TabHome(c.session, worktrees, projects)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: TabHome = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestTabsOfAWorktreeListLiveAgentsAndShellsInTheOrderTheyOpened(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	wt := "/h/api/b"
	sessions := []Session{
		{ID: "host", Harness: HarnessClaude, Pane: "%1", State: StateRunning, WorktreeIDs: []string{wt}, StartedAt: t0},
		{ID: "late", Harness: HarnessCodex, Pane: "%4", State: StatePermission, Tab: wt, StartedAt: t0.Add(3 * time.Minute)},
		{ID: "ended", Harness: HarnessCodex, Tab: wt, Ended: true, StartedAt: t0.Add(time.Minute)},
		{ID: "other", Harness: HarnessClaude, Pane: "%9", WorktreeIDs: []string{"/h/api/c"}, StartedAt: t0},
		{ID: "elsewhere", Harness: HarnessClaude, Pane: "%8", Tab: "/h/api/c", StartedAt: t0},
	}
	shells := []ShellTab{
		{ID: "sh2", Worktree: wt, Pane: "%3", At: t0.Add(2 * time.Minute)},
		{ID: "sh-other", Worktree: "/h/api/c", Pane: "%7", At: t0},
	}
	got := WorktreeTabs(wt, sessions, shells)
	want := []Tab{
		{ID: "host", Kind: TabAgent, Label: "claude", State: StateRunning, Pane: "%1"},
		{ID: "sh2", Kind: TabShell, Label: "shell", Pane: "%3"},
		{ID: "late", Kind: TabAgent, Label: "codex", State: StatePermission, Pane: "%4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tabs = %+v\nwant %+v", got, want)
	}
}

func TestTabsOpenedAtTheSameTimeKeepAStableOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	shells := []ShellTab{{ID: "b", Worktree: "w", Pane: "%2", At: t0}, {ID: "a", Worktree: "w", Pane: "%1", At: t0}}
	if got := tabIDs(WorktreeTabs("w", nil, shells)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("order = %v", got)
	}
}

func TestStepTabWrapsAroundTheStrip(t *testing.T) {
	tabs := []Tab{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	cases := []struct {
		active string
		delta  int
		want   string
	}{
		{"a", 1, "b"},
		{"c", 1, "a"},
		{"a", -1, "c"},
		{"b", -1, "a"},
		{"none", 1, "a"},
		{"none", -1, "c"},
		{"b", 0, "b"},
	}
	for _, c := range cases {
		got, ok := StepTab(tabs, c.active, c.delta)
		if !ok || got.ID != c.want {
			t.Errorf("StepTab(%s, %d) = %q, %v; want %q", c.active, c.delta, got.ID, ok, c.want)
		}
	}
	if _, ok := StepTab(nil, "a", 1); ok {
		t.Error("a step in an empty strip found a tab")
	}
}

func TestTabAfterCloseShowsANeighbourOnlyWhenTheShownTabCloses(t *testing.T) {
	tabs := []Tab{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	cases := []struct {
		name            string
		tabs            []Tab
		closing, active string
		want            string
		ok              bool
	}{
		{"another tab closes", tabs, "c", "a", "a", true},
		{"the shown middle tab closes", tabs, "b", "b", "c", true},
		{"the shown last tab closes", tabs, "c", "c", "b", true},
		{"the only tab closes", []Tab{{ID: "a"}}, "a", "a", "", false},
		{"nothing of the strip is shown", tabs, "b", "x", "", false},
	}
	for _, c := range cases {
		got, ok := TabAfterClose(c.tabs, c.closing, c.active)
		if got.ID != c.want || ok != c.ok {
			t.Errorf("%s: = %q, %v; want %q, %v", c.name, got.ID, ok, c.want, c.ok)
		}
	}
}

func TestTabStripMarksTheShownTabAndEachAgentsState(t *testing.T) {
	tabs := []Tab{
		{ID: "host", Kind: TabAgent, Label: "claude", State: StateRunning},
		{ID: "sh", Kind: TabShell, Label: "shell"},
		{ID: "x", Kind: TabAgent, Label: "codex", State: StatePermission},
	}
	if got, want := TabStrip(tabs, "sh"), "1 ◐ claude  [2 shell]  3 ✳ codex"; got != want {
		t.Errorf("strip = %q, want %q", got, want)
	}
	if got := TabStrip(tabs[:1], "host"); got != "[1 ◐ claude]" {
		t.Errorf("one tab = %q", got)
	}
}

func TestTabbedTitlePutsTheStripBeforeThePanesOwnTitle(t *testing.T) {
	if got := TabbedTitle("[1 ◐ claude]  2 shell", "◐ claude · opus"); got != "[1 ◐ claude]  2 shell ┃ ◐ claude · opus" {
		t.Errorf("title = %q", got)
	}
	if got := TabbedTitle("", "◐ claude · opus"); got != "◐ claude · opus" {
		t.Errorf("no strip = %q", got)
	}
}

func TestTabSessionsNeverTakeAWorktreeByCwdOrClaim(t *testing.T) {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	wt := ListedWorktree{Path: "/h/api/new", Branch: "new"}
	hints := []SessionHint{{ID: "tab", Cwd: "/h/api/new", Tab: true}}
	claims := []WorktreeClaim{{SessionID: "tab", Cwd: "/h/api/b", Command: "git worktree add /h/api/new", At: now, Tab: true}}
	if got := AttributeWorktree(wt, hints, claims, now); got != "" {
		t.Errorf("a tab session took a worktree: %q", got)
	}
	hints = append(hints, SessionHint{ID: "host", Cwd: "/h/api/new"})
	if got := AttributeWorktree(wt, hints, claims, now); got != "host" {
		t.Errorf("attributed to %q, want host", got)
	}
	sub := ListedWorktree{Path: "/h/api/b/.claude/worktrees/agent-1"}
	if got := AttributeWorktree(sub, []SessionHint{{ID: "tab", Cwd: "/h/api/b", Tab: true}}, nil, now); got != "" {
		t.Errorf("a tab session's subagent worktree went to %q", got)
	}
	known := []Worktree{{ID: "/h/api/b", Repo: "/code/api", Path: "/h/api/b", SessionID: "host"}, {ID: "/h/api/new", Repo: "/code/api", Path: "/h/api/new", Branch: "new"}}
	tabClaim := []WorktreeClaim{{SessionID: "tab", Cwd: "/h/api/b", Command: "git worktree add /h/api/new new", At: now, Tab: true}}
	if got := ReclaimWorktrees(known, tabClaim, now); len(got) != 0 {
		t.Errorf("a tab session's claim reclaimed %+v", got)
	}
}

func TestTabSessionMarksSessionsOpenedAsTabs(t *testing.T) {
	if !(Session{Tab: "/h/api/b"}).IsTab() || (Session{WorktreeIDs: []string{"/h/api/b"}}).IsTab() {
		t.Error("IsTab is wrong")
	}
}
