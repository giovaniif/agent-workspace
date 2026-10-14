package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type automationPrompt struct {
	list    []domain.Automation
	idx     int
	typing  bool
	text    string
	loading bool
	err     string
}

type automationsMsg struct {
	list []domain.Automation
	err  error
}

func (m Model) openAutomations() (Model, tea.Cmd) {
	c := m.opts.Calls
	if c == nil {
		return m, nil
	}
	m.auto = &automationPrompt{loading: true}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var list rpc.AutomationList
		if err := c.Call(ctx, rpc.MethodAutomationList, rpc.AutomationListParams{}, &list); err != nil {
			return automationsMsg{err: err}
		}
		var enabled []domain.Automation
		for _, a := range list.Automations {
			if a.Enabled {
				enabled = append(enabled, a)
			}
		}
		return automationsMsg{list: enabled}
	}
}

func (m Model) automationsLoaded(msg automationsMsg) Model {
	if m.auto == nil {
		return m
	}
	p := *m.auto
	p.loading, p.list = false, msg.list
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	m.auto = &p
	return m
}

func (m Model) automationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.auto
	m.auto = &p
	k := msg.String()
	if k == "esc" || k == "ctrl+c" {
		m.auto = nil
		return m, nil
	}
	if !p.typing {
		switch k {
		case "j", "down", "right":
			if len(p.list) > 0 {
				p.idx = (p.idx + 1) % len(p.list)
			}
		case "k", "up", "left":
			if len(p.list) > 0 {
				p.idx = (p.idx - 1 + len(p.list)) % len(p.list)
			}
		case "enter":
			if len(p.list) > 0 {
				p.typing = true
			}
		}
		return m, nil
	}
	switch k {
	case "enter":
		m.auto = nil
		name := p.list[p.idx].Name
		return m, m.call(rpc.MethodAutomationRun, rpc.AutomationRunParams{Name: name, Context: strings.TrimSpace(p.text)})
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

func (m Model) automationPaste(s string) Model {
	if !m.auto.typing {
		return m
	}
	p := *m.auto
	p.text += strings.ReplaceAll(s, "\n", " ")
	m.auto = &p
	return m
}

func (m Model) automationLeft() []piece {
	s := m.styles
	p := m.auto
	switch {
	case p.loading:
		return []piece{{s.badge, " AUTOMATION "}, {s.dim, " loading…"}}
	case p.err != "":
		return []piece{{s.badge, " AUTOMATION "}, {s.peach, " " + p.err}}
	case len(p.list) == 0:
		return []piece{{s.badge, " AUTOMATION "}, {s.dim, " no automations: add [[automation]] to config.toml"}}
	case !p.typing:
		a := p.list[p.idx]
		return []piece{{s.badge, " AUTOMATION "}, {s.text, " ‹ " + a.Name + " › "}, {s.dim, a.Agent + " · enter"}}
	}
	badge := " RUN " + p.list[p.idx].Name + " "
	room := max(m.width-ansi.StringWidth(badge)-ansi.StringWidth(" ▏"), 1)
	text := p.text
	for cut := ansi.StringWidth(p.text) - room; ansi.StringWidth(text) > room; cut++ {
		text = ansi.TruncateLeft(p.text, cut, "")
	}
	return []piece{{s.badge, badge}, {s.text, " " + text + "▏"}}
}
