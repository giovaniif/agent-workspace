package tui

import (
	"fmt"
	"strings"

	"github.com/giovaniif/crew/internal/domain"
)

const maxSubagentRows = 8

func (m *Model) putSubagent(sub domain.Subagent) {
	list := m.subagents[sub.SessionID]
	for i, s := range list {
		if s.ID == sub.ID {
			list[i] = sub
			return
		}
	}
	m.subagents[sub.SessionID] = append(list, sub)
}

func (m Model) subagentLines(sessionID string, sel bool) []string {
	s := m.styles
	bar := piece{s.text, " "}
	if sel {
		bar = piece{s.bar, "▌"}
	}
	var out []string
	running, done := 0, 0
	for _, n := range domain.SubagentTree(m.subagents[sessionID]) {
		if n.State != domain.SubagentRunning {
			done++
			continue
		}
		if running++; running > maxSubagentRows {
			continue
		}
		left := []piece{bar, {s.blue, "    " + strings.Repeat("  ", n.Depth) + spinner[m.frame%len(spinner)] + " "}}
		if label := cleanText(n.Type); label != "" {
			left = append(left, piece{s.text, label + " "})
		}
		left = append(left, piece{s.sub, cleanText(n.Summary)})
		out = append(out, m.line(sel, left, nil))
	}
	if extra := running - maxSubagentRows; extra > 0 {
		out = append(out, m.line(sel, []piece{bar, {s.dim, fmt.Sprintf("    … %d more", extra)}}, nil))
	}
	if sel && done > 0 {
		out = append(out, m.line(sel, []piece{bar, {s.green, "    ✓ "}, {s.dim, count(done, "subagent") + " done"}}, nil))
	}
	return out
}
