package domain

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TabKind string

const (
	TabAgent TabKind = "agent"
	TabShell TabKind = "shell"
)

type ShellTab struct {
	ID       string
	Worktree string
	Pane     string
	At       time.Time
}

type Tab struct {
	ID    string
	Kind  TabKind
	Label string
	State AgentState `json:",omitempty"`
	Pane  string
}

func (s Session) IsTab() bool { return s.Tab != "" }

func TabHome(s Session, worktrees []Worktree, projects []Project) (string, bool) {
	ids := s.WorktreeIDs
	if s.IsTab() {
		ids = []string{s.Tab}
	}
	for _, id := range ids {
		for _, w := range worktrees {
			if w.ID != id {
				continue
			}
			if _, ok := ProjectOfWorktree(projects, w); ok {
				return id, true
			}
		}
	}
	return "", false
}

func WorktreeTabs(worktree string, sessions []Session, shells []ShellTab) []Tab {
	type timed struct {
		tab Tab
		at  time.Time
	}
	var all []timed
	for _, s := range sessions {
		if s.Ended || s.Pane == "" {
			continue
		}
		if s.Tab != worktree && (s.IsTab() || !slices.Contains(s.WorktreeIDs, worktree)) {
			continue
		}
		all = append(all, timed{Tab{ID: s.ID, Kind: TabAgent, Label: string(s.Harness), State: s.State, Pane: s.Pane}, s.StartedAt})
	}
	for _, sh := range shells {
		if sh.Worktree == worktree {
			all = append(all, timed{Tab{ID: sh.ID, Kind: TabShell, Label: "shell", Pane: sh.Pane}, sh.At})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].at.Equal(all[j].at) {
			return all[i].at.Before(all[j].at)
		}
		return all[i].tab.ID < all[j].tab.ID
	})
	tabs := make([]Tab, 0, len(all))
	for _, t := range all {
		tabs = append(tabs, t.tab)
	}
	return tabs
}

func tabIndex(tabs []Tab, id string) int {
	return slices.IndexFunc(tabs, func(t Tab) bool { return t.ID == id })
}

func StepTab(tabs []Tab, active string, delta int) (Tab, bool) {
	n := len(tabs)
	if n == 0 {
		return Tab{}, false
	}
	i := tabIndex(tabs, active)
	if i < 0 {
		if delta < 0 {
			return tabs[n-1], true
		}
		return tabs[0], true
	}
	return tabs[((i+delta)%n+n)%n], true
}

func TabAfterClose(tabs []Tab, closing, active string) (Tab, bool) {
	a := tabIndex(tabs, active)
	if a < 0 {
		return Tab{}, false
	}
	if closing != active {
		return tabs[a], true
	}
	if a+1 < len(tabs) {
		return tabs[a+1], true
	}
	if a > 0 {
		return tabs[a-1], true
	}
	return Tab{}, false
}

func TabStrip(tabs []Tab, active string) string {
	parts := make([]string, 0, len(tabs))
	for i, t := range tabs {
		label := strconv.Itoa(i+1) + " "
		if t.Kind == TabAgent {
			label += stateMark(t.State) + " "
		}
		label += t.Label
		if t.ID == active {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

func TabbedTitle(strip, title string) string {
	if strip == "" {
		return title
	}
	return strip + " ┃ " + title
}
