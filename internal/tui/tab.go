package tui

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	ownTab    = "tb:"
	tabIndent = "    "
)

type tabChooser struct {
	session  string
	home     string
	row      int
	onEffort bool
	models   map[domain.Harness]string
	efforts  map[domain.Harness]string
}

type tabClose struct {
	session string
	tab     string
}

type tabStrip struct {
	home   string
	tabs   []domain.Tab
	active string
}

func (m Model) tabsOf(sessionID string) (tabStrip, bool) {
	x, found := m.sessions[sessionID]
	if !found || len(m.projects) == 0 {
		return tabStrip{}, false
	}
	worktrees := make([]domain.Worktree, 0, len(m.worktrees))
	for _, w := range m.worktrees {
		worktrees = append(worktrees, w)
	}
	projects := make([]domain.Project, 0, len(m.projects))
	for _, p := range m.projects {
		projects = append(projects, p)
	}
	home, ok := domain.TabHome(x, worktrees, projects)
	if !ok {
		return tabStrip{}, false
	}
	shells := make([]domain.ShellTab, 0, len(m.shellTabs))
	for _, sh := range m.shellTabs {
		shells = append(shells, sh)
	}
	tabs := domain.WorktreeTabs(home, sessionsOf(m.sessions), shells)
	if len(tabs) == 0 {
		return tabStrip{}, false
	}
	strip := tabStrip{home: home, tabs: tabs, active: m.activeTabs[home]}
	if !slices.ContainsFunc(tabs, func(t domain.Tab) bool { return t.ID == strip.active }) {
		strip.active = ""
	}
	return strip, true
}

func (t tabStrip) shown(selected string) (domain.Tab, int, bool) {
	for _, id := range []string{t.active, selected} {
		for i, tab := range t.tabs {
			if id != "" && tab.ID == id {
				return tab, i, true
			}
		}
	}
	return domain.Tab{}, 0, false
}

func (t tabStrip) chips() []string {
	return strings.Split(domain.TabStrip(t.tabs, t.active), "  ")
}

func (m Model) tabLine(sessionID string, sel bool, bar piece) (string, bool) {
	strip, ok := m.tabsOf(sessionID)
	if !ok {
		return "", false
	}
	s := m.styles
	pieces := []piece{bar, {s.dim, tabIndent}}
	for i, c := range strip.chips() {
		if i > 0 {
			pieces = append(pieces, piece{s.dim, "  "})
		}
		st := s.sub
		if strings.HasPrefix(c, "[") {
			st = s.bold
		}
		pieces = append(pieces, piece{st, c})
	}
	return m.line(sel, pieces, nil), true
}

func (m Model) tabAt(sessionID string, x int) (domain.Tab, bool) {
	strip, ok := m.tabsOf(sessionID)
	if !ok {
		return domain.Tab{}, false
	}
	col := 1 + len(tabIndent)
	for i, c := range strip.chips() {
		w := ansi.StringWidth(c)
		if x >= col && x < col+w {
			return strip.tabs[i], true
		}
		col += w + 2
	}
	return domain.Tab{}, false
}

func (m Model) clickTab(sessionID string, x int) (tea.Model, tea.Cmd) {
	m.selectInPlace(sessionID)
	tab, ok := m.tabAt(sessionID, x)
	if !ok {
		return m, nil
	}
	return m, m.call(rpc.MethodTabShow, rpc.TabParams{Session: sessionID, Tab: tab.ID})
}

func (m Model) tabKey(k string) (Model, tea.Cmd, bool) {
	strip, ok := m.tabsOf(m.selected)
	if !ok || m.opts.Calls == nil {
		return m, nil, k == "[" || k == "]" || k == "+" || k == "-"
	}
	switch k {
	case "[", "]":
		delta := 1
		if k == "[" {
			delta = -1
		}
		return m, m.call(rpc.MethodTabStep, rpc.TabParams{Session: m.selected, Delta: delta}), true
	case "+":
		m.tabNew = m.newTabChooser(strip.home)
		return m, nil, true
	case "-":
		tab, i, known := strip.shown(m.selected)
		if !known {
			return m, nil, true
		}
		m.closingTab = &tabClose{session: m.selected, tab: tab.ID}
		m.status = "close tab " + strconv.Itoa(i+1) + " " + tab.Label + "? y/n"
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) confirmCloseTab(k string) (tea.Model, tea.Cmd) {
	c := *m.closingTab
	m.closingTab, m.status = nil, ""
	if k != "y" {
		return m, nil
	}
	return m, m.call(rpc.MethodTabClose, rpc.TabParams{Session: c.session, Tab: c.tab})
}

func (m Model) newTabChooser(home string) *tabChooser {
	c := &tabChooser{session: m.selected, home: home, models: map[domain.Harness]string{}, efforts: map[domain.Harness]string{}}
	for _, h := range harnessChoices {
		d := m.opts.Defaults[domain.Harness(h)]
		c.models[domain.Harness(h)], c.efforts[domain.Harness(h)] = d.Model, d.Effort
	}
	return c
}

func (c *tabChooser) harness() (domain.Harness, bool) {
	if c.row == 0 {
		return "", false
	}
	return domain.Harness(harnessChoices[c.row-1]), true
}

func (m Model) tabModelChoices(h domain.Harness, current string) []string {
	base := domain.SwitchChoices(h, domain.SwitchModel)
	if len(base) == 0 {
		base = m.opts.ModelChoices[h]
	}
	out := append([]string{""}, base...)
	if current != "" && !slices.Contains(out, current) {
		out = append(out, current)
	}
	return out
}

func tabEffortChoices(h domain.Harness, current string) []string {
	out := effortsFor(h)
	if current != "" && !slices.Contains(out, current) {
		out = append(out, current)
	}
	return out
}

func (m Model) tabChooserKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := *m.tabNew
	c.models, c.efforts = maps.Clone(c.models), maps.Clone(c.efforts)
	m.tabNew = &c
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.tabNew = nil
	case "j", "down":
		c.row = min(c.row+1, len(harnessChoices))
	case "k", "up":
		c.row = max(c.row-1, 0)
	case "tab", "shift+tab":
		c.onEffort = !c.onEffort
	case "right", "l", "left", "h":
		h, agent := c.harness()
		if !agent {
			break
		}
		delta := 1
		if k := msg.String(); k == "left" || k == "h" {
			delta = -1
		}
		if c.onEffort {
			choices := tabEffortChoices(h, c.efforts[h])
			c.efforts[h] = choices[cycle(slices.Index(choices, c.efforts[h]), delta, len(choices))]
		} else {
			choices := m.tabModelChoices(h, c.models[h])
			c.models[h] = choices[cycle(slices.Index(choices, c.models[h]), delta, len(choices))]
		}
	case "enter":
		m.tabNew = nil
		p := rpc.TabParams{Session: c.session, Kind: string(domain.TabShell)}
		if h, agent := c.harness(); agent {
			p = rpc.TabParams{Session: c.session, Kind: string(domain.TabAgent), Harness: string(h), Model: c.models[h], Effort: c.efforts[h]}
		}
		return m, m.call(rpc.MethodTabNew, p)
	}
	return m, nil
}

func (m Model) tabChooserLines() []string {
	s := m.styles
	c := m.tabNew
	out := []string{"", m.line(false, []piece{{s.header, " NEW TAB"}, {s.dim, " in " + path.Base(c.home)}}, nil), ""}
	row := func(i int, label string, extra []piece) {
		sel := i == c.row
		bar := piece{s.text, " "}
		if sel {
			bar = piece{s.bar, "▌"}
		}
		out = append(out, m.line(sel, append([]piece{bar, {s.bold, " " + label}}, extra...), nil))
	}
	row(0, "shell", nil)
	for i, name := range harnessChoices {
		h := domain.Harness(name)
		model, effort := cmp.Or(c.models[h], "default"), cmp.Or(c.efforts[h], "default")
		mst, est := s.sub, s.sub
		if i+1 == c.row {
			if c.onEffort {
				est = s.bold
			} else {
				mst = s.bold
			}
		}
		row(i+1, padRight(name, 7), []piece{{mst, "‹ " + model + " ›"}, {s.dim, " "}, {est, "‹ " + effort + " ›"}})
	}
	return append(out, "", m.line(false, []piece{{s.bold, " enter"}, {s.dim, " open "}, {s.bold, " ←/→"}, {s.dim, " change "}, {s.bold, " tab"}, {s.dim, " model/effort "}, {s.bold, " esc"}, {s.dim, " back"}}, nil))
}
