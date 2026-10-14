package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type draftMsg struct {
	draft domain.ReviewDraft
	sent  bool
	err   error
}

func (r reviewState) rows(f reviewFile) []diffRow {
	if r.split {
		return f.split
	}
	return f.unified
}

func (m *Model) resetLine() {
	m.rv.line, m.rv.marking = 0, false
	if f, ok := m.rv.current(); ok && len(m.rv.rows(f)) > 1 {
		m.rv.line = 1
	}
}

func (m *Model) clampLine() {
	f, ok := m.rv.current()
	if !ok {
		m.rv.line = 0
		return
	}
	m.rv.line = min(max(m.rv.line, 0), max(len(m.rv.rows(f))-1, 0))
	m.follow()
}

func (m *Model) moveLine(delta int) {
	m.rv.line += delta
	m.clampLine()
}

func (m *Model) follow() {
	h := m.diffHeight() - 1
	if m.rv.line < m.rv.scroll {
		m.rv.scroll = m.rv.line
	}
	if m.rv.line >= m.rv.scroll+h {
		m.rv.scroll = m.rv.line - h + 1
	}
	m.rv.scroll = max(m.rv.scroll, 0)
}

func (r reviewState) selected() (int, int) {
	if !r.marking {
		return r.line, r.line
	}
	return min(r.mark, r.line), max(r.mark, r.line)
}

func (r reviewState) selectedLines(f reviewFile) []domain.DiffLine {
	rows := r.rows(f)
	from, to := r.selected()
	seen := map[*styledLine]bool{}
	for i := from; i <= to && i < len(rows); i++ {
		for _, sl := range []*styledLine{rows[i].one, rows[i].left, rows[i].right} {
			if sl != nil {
				seen[sl] = true
			}
		}
	}
	var lines []domain.DiffLine
	for _, row := range f.unified {
		if row.one != nil && seen[row.one] {
			lines = append(lines, row.one.line)
		}
	}
	return lines
}

func (m Model) commentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rv.typing, m.rv.text = false, ""
		return m, nil
	case "enter":
		return m.submitComment()
	case "ctrl+j":
		m.rv.text += "\n"
	case "backspace":
		if r := []rune(m.rv.text); len(r) > 0 {
			m.rv.text = string(r[:len(r)-1])
		}
	default:
		m.rv.text += msg.Text
	}
	return m, nil
}

func (m Model) submitComment() (tea.Model, tea.Cmd) {
	text := m.rv.text
	m.rv.typing, m.rv.text = false, ""
	f, ok := m.rv.current()
	if !ok {
		return m, nil
	}
	lines := m.rv.selectedLines(f)
	c := domain.CommentOn(f.wt.Path, f.file.Path, lines, text)
	if c.Body == "" || len(lines) == 0 {
		return m, nil
	}
	m.rv.marking = false
	r := m.opts.Review
	p := rpc.CommentParams{Session: m.rv.session, Worktree: f.wt.ID, Path: c.Path, StartLine: c.Start, EndLine: c.End,
		Code: strings.Join(c.Code, "\n"), Body: c.Body, Removed: c.Removed}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d, err := r.AddReviewComment(ctx, p)
		return draftMsg{draft: d, err: err}
	}
}

func (m *Model) sendDraft() tea.Cmd {
	if m.draftCount(m.rv.session) == 0 {
		return nil
	}
	r, session := m.opts.Review, m.rv.session
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d, err := r.SendReview(ctx, session)
		return draftMsg{draft: d, sent: true, err: err}
	}
}

func (m Model) gotDraft(msg draftMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}
	if msg.draft.Session != "" && msg.draft.Session != m.rv.session {
		return m, nil
	}
	if msg.draft.Session == "" {
		msg.draft.Session = m.rv.session
	}
	m.putDraft(msg.draft)
	switch {
	case !msg.sent:
		m.status = ""
	case msg.draft.Status == domain.DraftSent:
		m.status = "sent " + count(len(msg.draft.Comments), "comment") + " to the agent"
	default:
		m.status = "queued: sends when the agent is between tools"
	}
	return m, nil
}

func (m Model) cursorHunk() (int, bool) {
	f, ok := m.rv.current()
	if !ok {
		return 0, false
	}
	rows := m.rv.rows(f)
	if m.rv.line >= len(rows) || f.file.Binary {
		return 0, false
	}
	return rows[m.rv.line].hunk, true
}

func (m *Model) applyHunk(a domain.HunkAction) tea.Cmd {
	h, ok := m.cursorHunk()
	if !ok {
		return nil
	}
	f, _ := m.rv.current()
	r := m.opts.Review
	p := rpc.HunkParams{Session: m.rv.session, Worktree: f.wt.ID, File: f.file, Hunk: h, Action: a}
	refetch := m.fetchReview()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewCallTimeout)
		defer cancel()
		if err := r.ApplyHunk(ctx, p); err != nil {
			return errMsg{err}
		}
		return refetch()
	}
}
