package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type Switcher interface {
	SwitchSession(ctx context.Context, sessionID string, kind domain.SwitchKind, value string) (domain.Session, error)
}

type picker struct {
	sessionID string
	harness   domain.Harness
	kind      domain.SwitchKind
	choices   []string
	cursor    int
	// why: a harness with no model list takes the id typed here instead.
	typed bool
	text  string
	query string
}

func (m Model) openPicker(kind domain.SwitchKind) Model {
	i := m.index(m.selected)
	if i < 0 || m.opts.Switch == nil {
		return m
	}
	s := m.entries[i].session
	if !domain.SwitchSupported(s.Harness) {
		m.status = fmt.Sprintf("switching is not supported for %s", s.Harness)
		return m
	}
	choices := domain.SwitchChoices(s.Harness, kind)
	if len(choices) == 0 && kind == domain.SwitchModel {
		choices = m.opts.ModelChoices[s.Harness]
	}
	m.picker = &picker{sessionID: s.ID, harness: s.Harness, kind: kind, choices: choices, typed: len(choices) == 0}
	return m
}

func (p picker) shown() []string {
	if p.typed || p.kind != domain.SwitchModel || p.query == "" {
		return p.choices
	}
	return matchingModels(p.choices, p.query)
}

func (m Model) pickerKey(k string) (tea.Model, tea.Cmd) {
	p := *m.picker
	if p.typed {
		return m.typedPickerKey(p, k)
	}
	vis := p.shown()
	switch k {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.picker = nil
	case "j", "down":
		if n := len(vis); n > 0 {
			p.cursor = min(p.cursor+1, n-1)
		}
		m.picker = &p
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
		m.picker = &p
	case "enter":
		if p.cursor >= 0 && p.cursor < len(vis) {
			return m.applyChoice(p, vis[p.cursor])
		}
		if p.kind == domain.SwitchModel && domain.Spec(p.harness).Models == nil {
			if v := strings.TrimSpace(p.query); v != "" {
				return m.applyChoice(p, v)
			}
		}
		return m, nil
	case "backspace":
		if p.kind == domain.SwitchModel && p.query != "" {
			r := []rune(p.query)
			p.query = string(r[:len(r)-1])
			p.cursor = 0
			m.picker = &p
		}
	default:
		if p.kind == domain.SwitchModel && len(k) == 1 && k[0] >= ' ' && (p.query != "" || k[0] < '1' || k[0] > '9') {
			p.query += k
			p.cursor = 0
			m.picker = &p
			return m, nil
		}
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && int(k[0]-'1') < len(vis) {
			return m.applyChoice(p, vis[k[0]-'1'])
		}
	}
	return m, nil
}

func (m Model) typedPickerKey(p picker, k string) (tea.Model, tea.Cmd) {
	switch k {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.picker = nil
		return m, nil
	case "enter":
		if v := strings.TrimSpace(p.text); v != "" {
			return m.applyChoice(p, v)
		}
		return m, nil
	case "backspace":
		r := []rune(p.text)
		p.text = string(r[:max(len(r)-1, 0)])
	default:
		if utf8.RuneCountInString(k) == 1 {
			p.text += k
		}
	}
	m.picker = &p
	return m, nil
}

func (m Model) applyChoice(p picker, value string) (tea.Model, tea.Cmd) {
	m.picker = nil
	sw := m.opts.Switch
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := sw.SwitchSession(ctx, p.sessionID, p.kind, value); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) pickerLines() []string {
	p := m.picker
	s := m.styles
	title := " MODEL"
	if p.kind == domain.SwitchEffort {
		title = " EFFORT"
	}
	out := []string{"", m.line(false, []piece{{s.header, title}, {s.sub, fmt.Sprintf(" · %s", p.harness)}}, nil)}
	if p.typed {
		return append(out,
			m.line(true, []piece{{s.bold, "▌" + p.text}, {s.text, "▏"}}, nil),
			"", m.line(false, []piece{{s.dim, " type a model id · ⏎ apply · esc cancel"}}, nil))
	}
	if p.query != "" && p.kind == domain.SwitchModel {
		out = append(out, m.line(true, []piece{{s.bold, " " + p.query}, {s.text, "▏"}}, nil))
	}
	choices := p.shown()
	if len(choices) == 0 {
		out = append(out, m.line(false, []piece{{s.dim, " no match"}}, nil))
	}
	for i, c := range choices {
		label := piece{s.text, fmt.Sprintf(" %d  %s", i+1, c)}
		if i == p.cursor {
			label = piece{s.bold, fmt.Sprintf("▌%d  %s", i+1, c)}
		}
		out = append(out, m.line(i == p.cursor, []piece{label}, nil))
	}
	out = append(out, "", m.line(false, []piece{{s.dim, " j/k or 1-9 pick · ⏎ apply · esc cancel"}}, nil))
	return out
}

func (m Model) switchMarks(x domain.Session) []piece {
	var marks []piece
	for _, sw := range x.Switches {
		marks = append(marks, piece{m.styles.peach, "→ " + sw.Value + " "})
	}
	if x.SwitchWarning {
		marks = append(marks, piece{m.styles.need, "! "})
	}
	return marks
}
