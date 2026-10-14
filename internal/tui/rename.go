package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type renamePrompt struct {
	id   string
	text string
}

func (m Model) askRename() Model {
	i := m.index(m.selected)
	if i < 0 || m.opts.Calls == nil {
		return m
	}
	e := m.entries[i]
	m.renaming = &renamePrompt{id: e.session.ID, text: domain.NameFor(e.task, entryPRs(e))}
	return m
}

func entryPRs(e entry) []domain.PullRequest {
	var prs []domain.PullRequest
	for _, w := range e.worktrees {
		if w.PR != nil {
			prs = append(prs, *w.PR)
		}
	}
	return prs
}

func (m Model) renameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.renaming
	m.renaming = &p
	switch msg.String() {
	case "esc", "ctrl+c":
		m.renaming = nil
	case "enter":
		m.renaming = nil
		if name := strings.TrimSpace(p.text); name != "" {
			return m, m.call(rpc.MethodSessionRename, rpc.SessionRenameParams{ID: p.id, Name: name})
		}
	case "ctrl+u":
		p.text = ""
	case "backspace":
		if r := []rune(p.text); len(r) > 0 {
			p.text = string(r[:len(r)-1])
		}
	default:
		p.text += strings.ReplaceAll(msg.Text, "\n", " ")
	}
	return m, nil
}

func (m Model) renamePaste(s string) Model {
	p := *m.renaming
	p.text += strings.ReplaceAll(s, "\n", " ")
	m.renaming = &p
	return m
}

func (m Model) unpin() tea.Cmd {
	i := m.index(m.selected)
	if i < 0 || m.entries[i].task.PinnedName == "" {
		return nil
	}
	return m.call(rpc.MethodSessionUnpin, rpc.SessionRef{ID: m.selected})
}

func (m Model) renameLeft() []piece {
	s := m.styles
	const badge = " RENAME "
	room := max(m.width-ansi.StringWidth(badge)-ansi.StringWidth(" ▏"), 1)
	full := m.renaming.text
	text := full
	for cut := ansi.StringWidth(full) - room; ansi.StringWidth(text) > room; cut++ {
		text = ansi.TruncateLeft(full, cut, "")
	}
	return []piece{{s.badge, badge}, {s.text, " " + text + "▏"}}
}
