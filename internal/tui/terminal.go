package tui

import (
	"context"
	"errors"
	"net"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const terminalCallTimeout = 5 * time.Second

func (m Model) reviewWorktree() string {
	if m.rv.session == m.selected {
		return m.rv.worktree
	}
	return ""
}

func (m Model) callDaemon(method string, params any) tea.Cmd {
	c := m.opts.Calls
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), terminalCallTimeout)
		defer cancel()
		if err := c.Call(ctx, method, params, nil); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) toggleShell(popup bool) tea.Cmd {
	if m.opts.Calls == nil || m.selected == "" {
		return nil
	}
	return m.callDaemon(rpc.MethodShellToggle, rpc.ShellParams{Session: m.selected, Worktree: m.reviewWorktree(), Popup: popup})
}

func (m Model) toggleNvim() tea.Cmd {
	if m.opts.Calls == nil || m.selected == "" {
		return nil
	}
	return m.callDaemon(rpc.MethodNvimToggle, rpc.NvimParams{Session: m.selected, Worktree: m.reviewWorktree()})
}

func (m Model) openInNvim() (tea.Model, tea.Cmd) {
	f, ok := m.rv.current()
	if !ok || m.opts.Calls == nil {
		return m, nil
	}
	m.rv.open = false
	r := m.opts.Review
	open := m.callDaemon(rpc.MethodNvimOpen, rpc.NvimParams{
		Session: m.rv.session, Worktree: f.wt.ID, Path: f.file.Path, Line: m.topLine(f),
	})
	return m, func() tea.Msg {
		if msg := layout(r, false)(); msg != nil {
			return msg
		}
		return open()
	}
}

func (m Model) topLine(f reviewFile) int {
	rows := m.rv.rows(f)
	for i := max(m.rv.line, 0); i < len(rows); i++ {
		if n := newLine(rows[i]); n > 0 {
			return n
		}
	}
	for i := min(m.rv.line, len(rows)) - 1; i >= 0; i-- {
		if n := newLine(rows[i]); n > 0 {
			return n
		}
	}
	return 1
}

func newLine(r diffRow) int {
	switch {
	case r.one != nil:
		return r.one.line.New
	case r.right != nil:
		return r.right.line.New
	}
	return 0
}

func (m Model) draftCount(session string) int { return len(m.drafts[session].Comments) }

func (m *Model) putDraft(d domain.ReviewDraft) {
	if d.Status == domain.DraftOpen || d.Status == domain.DraftQueued {
		m.drafts[d.Session] = d
		return
	}
	if m.drafts[d.Session].ID == d.ID {
		delete(m.drafts, d.Session)
	}
}

func (m Model) focusShell() tea.Cmd {
	if m.opts.Calls == nil || m.selected == "" {
		return nil
	}
	return m.callDaemon(rpc.MethodShellFocus, rpc.ShellParams{Session: m.selected, Worktree: m.reviewWorktree()})
}

func (m Model) leave() tea.Cmd {
	c := m.opts.Calls
	if c == nil {
		return tea.Quit
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), terminalCallTimeout)
		defer cancel()
		err := c.Call(ctx, rpc.MethodClientDetach, struct{}{}, nil)
		var rerr *rpc.Error
		switch {
		case errors.As(err, &rerr) && (rerr.Code == rpc.CodeNotFound || rerr.Code == rpc.CodeVersionMismatch):
			return tea.QuitMsg{}
		case lostConnection(err):
			return tea.QuitMsg{}
		case err != nil:
			return errMsg{err}
		}
		return nil
	}
}

func lostConnection(err error) bool {
	var op *net.OpError
	return errors.Is(err, rpc.ErrClosed) || errors.As(err, &op)
}

func (m Model) quitKey(k string) bool {
	if k == "ctrl+c" {
		return true
	}
	return k == "q" && !m.typing() && m.ob == nil
}
