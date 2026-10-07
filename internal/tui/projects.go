package tui

import (
	"context"
	"maps"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const ownProject = "pr:"

const projectEnv = "AGENTWS_PROJECT"

type projectsPanel struct {
	cursor   int
	removing string
	adding   *projectForm
	seq      int
}

type projectForm struct {
	path, setup string
	onSetup     bool
	busy        bool
	err         string
	seq         int
}

type projectAddedMsg struct{ seq int }

type projectAddFailedMsg struct {
	seq int
	err error
}

func (m Model) projectRows() []domain.ProjectRow {
	projects := make([]domain.Project, 0, len(m.projects))
	for _, p := range m.projects {
		projects = append(projects, p)
	}
	worktrees := make([]domain.Worktree, 0, len(m.worktrees))
	for _, w := range m.worktrees {
		worktrees = append(worktrees, w)
	}
	return domain.ProjectRows(projects, worktrees)
}

func (m Model) projectSection() (lines, owners []string) {
	if len(m.projects) == 0 {
		return nil, nil
	}
	s := m.styles
	lines = append(lines, m.rule("PROJECTS", nil))
	owners = append(owners, "")
	rows := m.projectRows()
	room := max(m.height/3-2, 1)
	hidden := 0
	if len(rows) > room {
		hidden = len(rows) - room + 1
		rows = rows[:room-1]
	}
	for _, r := range rows {
		var right []piece
		if r.Worktrees > 0 {
			right = []piece{{s.dim, count(r.Worktrees, "worktree") + " "}}
		}
		lines = append(lines, m.line(false, []piece{{s.dim, " ▸ "}, {s.text, r.Project.Name}}, right))
		owners = append(owners, ownProject+r.Project.Root)
	}
	if hidden > 0 {
		lines = append(lines, m.line(false, []piece{{s.dim, " + " + strconv.Itoa(hidden) + " more · P lists all"}}, nil))
		owners = append(owners, "")
	}
	return append(lines, ""), append(owners, "")
}

func (m Model) openProjects() Model {
	m.launches++
	m.proj = &projectsPanel{seq: m.launches}
	return m
}

func (m Model) projectsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.proj
	m.proj = &p
	if p.adding != nil {
		return m.projectFormKey(msg)
	}
	k := msg.String()
	rows := m.projectRows()
	if p.removing != "" {
		root := p.removing
		p.removing = ""
		if k == "y" {
			return m, m.call(rpc.MethodProjectRemove, rpc.ProjectRemoveParams{Root: root})
		}
		return m, nil
	}
	switch k {
	case "esc", "q", "P":
		m.proj = nil
	case "j", "down":
		p.cursor = min(p.cursor+1, max(len(rows)-1, 0))
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
	case "a":
		m.launches++
		p.adding = &projectForm{seq: m.launches}
	case "d":
		if p.cursor < len(rows) {
			p.removing = rows[p.cursor].Project.Root
		}
	case "enter":
		if p.cursor < len(rows) {
			m.proj = nil
			return m.openProject(rows[p.cursor].Project.Root)
		}
	}
	return m, nil
}

func (m Model) projectFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := *m.proj.adding
	m.proj.adding = &f
	switch msg.String() {
	case "esc", "ctrl+c":
		m.proj.adding = nil
		return m, nil
	}
	if f.busy {
		return m, nil
	}
	switch msg.String() {
	case "enter":
		return m, m.addProject()
	case "tab", "shift+tab", "down", "up":
		f.onSetup = !f.onSetup
	case "ctrl+u":
		f.setField("")
	case "backspace":
		if r := []rune(f.field()); len(r) > 0 {
			f.setField(string(r[:len(r)-1]))
		}
	default:
		f.setField(f.field() + strings.ReplaceAll(msg.Text, "\n", " "))
	}
	f.err = ""
	return m, nil
}

func (f *projectForm) field() string {
	if f.onSetup {
		return f.setup
	}
	return f.path
}

func (f *projectForm) setField(v string) {
	if f.onSetup {
		f.setup = v
	} else {
		f.path = v
	}
}

func (m Model) projectPaste(s string) Model {
	p := *m.proj
	if p.adding == nil {
		return m
	}
	f := *p.adding
	if !f.busy {
		f.setField(f.field() + strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " "))
		f.err = ""
	}
	p.adding = &f
	m.proj = &p
	return m
}

func (m Model) addProject() tea.Cmd {
	f := m.proj.adding
	c := m.opts.Calls
	if c == nil {
		f.err = "not connected to the daemon"
		return nil
	}
	path := strings.TrimSpace(f.path)
	if path == "" {
		f.err = "type the project's folder"
		return nil
	}
	f.busy = true
	seq := f.seq
	params := rpc.ProjectAddParams{Path: path, Setup: strings.TrimSpace(f.setup)}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := c.Call(ctx, rpc.MethodProjectAdd, params, nil); err != nil {
			return projectAddFailedMsg{seq, err}
		}
		return projectAddedMsg{seq}
	}
}

func (m Model) projectAddDone(seq int, err error) Model {
	if m.proj == nil || m.proj.adding == nil || m.proj.adding.seq != seq {
		return m
	}
	p := *m.proj
	if err == nil {
		p.adding = nil
	} else {
		f := *p.adding
		f.busy, f.err = false, err.Error()
		p.adding = &f
	}
	m.proj = &p
	return m
}

func (m Model) openProject(root string) (tea.Model, tea.Cmd) {
	switch {
	case m.opts.Calls == nil:
		return m, nil
	case len(m.opts.DialogPopup.Command) > 0:
		p := m.opts.DialogPopup
		p.Env = maps.Clone(p.Env)
		if p.Env == nil {
			p.Env = map[string]string{}
		}
		p.Env[projectEnv] = root
		return m, m.popup(p, root)
	}
	return m.openDialogIn(root), nil
}

func (m Model) projectPanelSelRow() int {
	return 3 + m.proj.cursor
}

func (m Model) projectPanelLines() []string {
	s := m.styles
	p := m.proj
	if f := p.adding; f != nil {
		out := []string{"", m.line(false, []piece{{s.header, " ADD PROJECT"}}, nil), ""}
		for _, fl := range []struct {
			label, value string
			on           bool
		}{{"path ", f.path, !f.onSetup}, {"setup", f.setup, f.onSetup}} {
			cursor, st := "", s.sub
			if fl.on {
				cursor, st = "▏", s.text
			}
			out = append(out, m.line(false, []piece{{s.dim, " " + fl.label + " "}, {st, cleanText(fl.value) + cursor}}, nil))
		}
		out = append(out, "", m.line(false, []piece{{s.dim, " the script runs with sh -c in each new worktree"}}, nil))
		if f.err != "" {
			out = append(out, m.line(false, []piece{{s.peach, " " + cleanText(f.err)}}, nil))
		}
		return append(out, "", m.line(false, []piece{{s.bold, " enter"}, {s.dim, " add "}, {s.bold, " tab"}, {s.dim, " field "}, {s.bold, " esc"}, {s.dim, " back"}}, nil))
	}
	out := []string{"", m.line(false, []piece{{s.header, " PROJECTS"}}, nil), ""}
	rows := m.projectRows()
	if len(rows) == 0 {
		out = append(out, m.line(false, []piece{{s.sub, " No projects yet."}}, nil))
	}
	for i, r := range rows {
		sel := i == p.cursor
		bar := piece{s.text, " "}
		if sel {
			bar = piece{s.bar, "▌"}
		}
		room := m.width - ansi.StringWidth(r.Project.Name) - 6
		var right []piece
		if room > 4 {
			right = []piece{{s.dim, ansi.TruncateLeft(r.Project.Root, max(ansi.StringWidth(r.Project.Root)-room+1, 0), "…") + " "}}
		}
		out = append(out, m.line(sel, []piece{bar, {s.bold, " " + r.Project.Name}}, right))
		if sel && r.Project.Setup != "" {
			out = append(out, m.line(sel, []piece{bar, {s.dim, "   setup: " + cleanText(r.Project.Setup)}}, nil))
		}
	}
	if p.removing != "" {
		name := p.removing
		if pr, found := m.projects[p.removing]; found {
			name = pr.Name
		}
		return append(out, "", m.line(false, []piece{{s.peach, " remove project " + name + "? y/n"}}, nil))
	}
	return append(out, "", m.line(false, []piece{{s.bold, " enter"}, {s.dim, " open "}, {s.bold, " a"}, {s.dim, " add "}, {s.bold, " d"}, {s.dim, " remove "}, {s.bold, " esc"}, {s.dim, " close"}}, nil))
}
