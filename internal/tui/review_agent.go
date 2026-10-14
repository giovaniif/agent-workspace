package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/crew/internal/domain"
)

const agentColumnFrom = 120

func (m Model) agentColumnWidth() int {
	if m.width < agentColumnFrom {
		return 0
	}
	return min(max(m.width/4, 30), 44)
}

func (m Model) agentColumn(width, height int) []string {
	t := m.opts.Theme
	p := m.paint
	x := m.sessions[m.rv.session]
	var wts []domain.Worktree
	for _, id := range x.WorktreeIDs {
		if w, ok := m.worktrees[id]; ok {
			wts = append(wts, w)
		}
	}
	card := domain.BuildSessionCard(m.tasks[x.TaskID], x, wts, m.events[x.ID])
	lines := []string{
		p.cell(width, "", seg{text: " " + agentStateMark(x.State) + " ", fg: t.Peach}, seg{text: card.Name, bold: true}, seg{text: "  " + agentStateText(x.State), fg: t.Subtext}),
		p.cell(width, ""),
		p.cell(width, "", seg{text: strings.Repeat("─", width), fg: t.Surface}),
	}
	actions := slices.Clone(card.Actions)
	slices.Reverse(actions)
	for _, a := range actions {
		tool, detail, _ := strings.Cut(a, ": ")
		lines = append(lines, p.cell(width, "", seg{text: " ● ", fg: t.Green}, seg{text: tool, bold: true}, seg{text: " " + detail}))
	}
	if card.Said != "" {
		lines = append(lines, p.cell(width, ""))
		for _, l := range wrap(card.Said, width-2) {
			lines = append(lines, p.cell(width, "", seg{text: " " + l}))
		}
	}
	box := m.draftBox(width)
	for len(lines) < height-len(box) {
		lines = append(lines, p.cell(width, ""))
	}
	return append(lines[:max(height-len(box), 0)], box...)
}

func (m Model) draftBox(width int) []string {
	t := m.opts.Theme
	p := m.paint
	d, ok := m.drafts[m.rv.session]
	if !ok || d.Status == domain.DraftSent || len(d.Comments) == 0 {
		return nil
	}
	trees := map[string]bool{}
	for _, c := range d.Comments {
		trees[c.Worktree] = true
	}
	return []string{
		p.cell(width, "", seg{text: " Draft review → this session", fg: t.Blue, bold: true}),
		p.cell(width, "", seg{text: " " + count(len(d.Comments), "comment") + " · " + count(len(trees), "worktree") + " · sent as one prompt", fg: t.Subtext}),
		p.cell(width, ""),
	}
}

func agentStateMark(st domain.AgentState) string {
	switch st {
	case domain.StateRunning:
		return "◐"
	case domain.StateWaiting, domain.StatePermission:
		return "✳"
	case domain.StateDone:
		return "●"
	}
	return "○"
}

func agentStateText(st domain.AgentState) string {
	switch st {
	case domain.StateRunning:
		return "working"
	case domain.StateWaiting, domain.StatePermission:
		return "waiting for you"
	case domain.StateDone:
		return "done"
	}
	return "idle"
}

func wrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}
