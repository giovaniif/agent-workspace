package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

// why: with no data it is empty: the slot is hidden, never shown as zero.
func (m Model) limitLines() []string {
	quotas := m.quotas()
	now := m.opts.Now()
	var lines []string
	for _, h := range domain.Harnesses() {
		if line, ok := m.limitLine(h, quotas, now); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

func (m Model) limitLine(h domain.Harness, quotas []domain.Quota, now time.Time) (string, bool) {
	s := m.styles
	tag := m.harnessTag(h, " ")
	red := lipgloss.NewStyle().Foreground(lipgloss.Color(m.opts.Theme.Red)).Bold(true)
	left := []piece{tag}
	var oldest time.Duration
	for _, q := range quotas {
		if q.Harness != h {
			continue
		}
		text, figure := s.text, s.text
		if q.Stale(now) {
			text, figure = s.dim, s.dim
			oldest = max(oldest, q.Age(now))
		}
		if q.Low() {
			figure = red
		}
		if len(left) > 1 {
			left = append(left, piece{s.text, "  "})
		}
		left = append(left, piece{text, domain.WindowLabel(q.Window) + " "}, piece{figure, fmt.Sprintf("%d%%", usedPercent(q))})
		if reset := resetClock(q.ResetsAt, now); reset != "" {
			left = append(left, piece{text, " ↻" + reset})
		}
	}
	if len(left) == 1 {
		return "", false
	}
	var right []piece
	if oldest > 0 {
		right = []piece{{s.dim, shortDuration(oldest) + " ago "}}
	}
	return m.line(false, left, right), true
}

// why: claude.ai and Codex show percent used; the domain keeps percent left
// because the warning and fallback thresholds are set in it.
func usedPercent(q domain.Quota) int { return 100 - q.LeftPercent }

func resetClock(resetsAt int64, now time.Time) string {
	if resetsAt == 0 {
		return ""
	}
	at := time.Unix(resetsAt, 0).In(now.Location())
	if at.Sub(now) >= 24*time.Hour {
		return at.Format("Mon")
	}
	return at.Format("15:04")
}

func shortDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		if m := int(d % time.Hour / time.Minute); m > 0 {
			return fmt.Sprintf("%dh%dm", int(d/time.Hour), m)
		}
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}
