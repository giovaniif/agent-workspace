package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type resumePicker struct {
	choices []domain.Session
	cursor  int
}

func (m Model) openResume() Model {
	if m.opts.Calls == nil {
		return m
	}
	sessions := make([]domain.Session, 0, len(m.sessions))
	for _, x := range m.sessions {
		sessions = append(sessions, x)
	}
	slices.SortFunc(sessions, func(a, b domain.Session) int { return strings.Compare(a.ID, b.ID) })
	choices := domain.ResumableSessions(sessions, m.events)
	if len(choices) == 0 {
		m.status = "no ended session to resume"
		return m
	}
	m.resuming = &resumePicker{choices: choices}
	return m
}

func (m Model) resumeKey(k string) (tea.Model, tea.Cmd) {
	p := *m.resuming
	switch k {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.resuming = nil
	case "j", "down":
		p.cursor = min(p.cursor+1, len(p.choices)-1)
		m.resuming = &p
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
		m.resuming = &p
	case "enter":
		return m.resume(p.choices[p.cursor].ID)
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && int(k[0]-'1') < len(p.choices) {
			return m.resume(p.choices[k[0]-'1'].ID)
		}
	}
	return m, nil
}

func (m Model) resume(id string) (tea.Model, tea.Cmd) {
	m.resuming = nil
	call, show := m.call(rpc.MethodResumeSession, rpc.SessionRef{ID: id}), m.showNew(id)
	return m, func() tea.Msg {
		if msg := call(); msg != nil {
			return msg
		}
		return show()
	}
}

func (m Model) resumeLines() []string {
	s := m.styles
	out := []string{"", m.line(false, []piece{{s.header, " RESUME"}, {s.sub, " · ended sessions"}}, nil)}
	for i, x := range m.resuming.choices {
		label := fmt.Sprintf("%d  %s", i+1, domain.NameFor(m.tasks[x.TaskID], nil))
		where := fmt.Sprintf(" %s · %s", x.Harness, filepath.Base(x.Dir))
		if i == m.resuming.cursor {
			out = append(out, m.line(true, []piece{{s.bold, "▌" + label}, {s.sub, where}}, nil))
			continue
		}
		out = append(out, m.line(false, []piece{{s.text, " " + label}, {s.dim, where}}, nil))
	}
	return append(out, "", m.line(false, []piece{{s.dim, " j/k or 1-9 pick · ⏎ resume · esc cancel"}}, nil))
}
