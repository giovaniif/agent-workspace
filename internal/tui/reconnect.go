package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type Daemon interface {
	Focuser
	Attender
	Killer
	Caller
	Switcher
	Reviewer
	Disker
	Onboarder
}

type Connection struct {
	State  rpc.State
	Diffs  <-chan rpc.Diff
	Daemon Daemon
}

type RedialMsg struct{}

type redialedMsg struct {
	conn Connection
	err  error
}

type streamDiffMsg struct {
	diff  rpc.Diff
	diffs <-chan rpc.Diff
}

type reconnect struct {
	attempt  int
	at       time.Time
	dialing  bool
	upgraded bool
}

const redialTimeout = 3 * time.Second

var ErrRestart = errors.New("tui: restart on the new binary")

func (m Model) Restart() bool { return m.restart }

func listen(diffs <-chan rpc.Diff) tea.Cmd {
	if diffs == nil {
		return nil
	}
	return func() tea.Msg {
		d, ok := <-diffs
		if !ok {
			return DisconnectedMsg{}
		}
		return streamDiffMsg{d, diffs}
	}
}

func (m Model) disconnect() (Model, tea.Cmd) {
	if m.disconnected {
		return m, nil
	}
	m.disconnected, m.rc = true, reconnect{}
	if m.opts.Redial == nil {
		return m, nil
	}
	return m.scheduleRedial()
}

func (m Model) scheduleRedial() (Model, tea.Cmd) {
	m.rc.attempt++
	m.rc.dialing = false
	d := domain.ReconnectDelay(m.rc.attempt)
	m.rc.at = m.opts.Now().Add(d)
	return m, tea.Tick(d, func(time.Time) tea.Msg { return RedialMsg{} })
}

func (m Model) redial() (Model, tea.Cmd) {
	dial := m.opts.Redial
	if !m.disconnected || m.rc.upgraded || m.rc.dialing || dial == nil {
		return m, nil
	}
	m.rc.dialing = true
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), redialTimeout)
		defer cancel()
		conn, err := dial(ctx)
		return redialedMsg{conn, err}
	}
}

func (m Model) redialed(msg redialedMsg) (tea.Model, tea.Cmd) {
	var rerr *rpc.Error
	switch {
	case errors.As(msg.err, &rerr) && rerr.Code == rpc.CodeVersionMismatch:
		m.rc.dialing, m.rc.upgraded = false, true
		return m, nil
	case msg.err != nil:
		return m.scheduleRedial()
	}
	m.disconnected, m.rc = false, reconnect{}
	m.use(msg.conn.Daemon)
	next, cmd := m.Update(StateMsg(msg.conn.State))
	return next, tea.Batch(cmd, listen(msg.conn.Diffs))
}

func (m *Model) use(d Daemon) {
	if d == nil {
		return
	}
	o := &m.opts
	if o.Focus != nil {
		o.Focus = d
	}
	if o.Attend != nil {
		o.Attend = d
	}
	if o.Kill != nil {
		o.Kill = d
	}
	if o.Calls != nil {
		o.Calls = d
	}
	if o.Switch != nil {
		o.Switch = d
	}
	if o.Review != nil {
		o.Review = d
	}
	if o.Disk != nil {
		o.Disk = d
	}
	if o.Onboard != nil {
		o.Onboard = d
	}
}

func (m Model) banner() []string {
	if !m.disconnected {
		return nil
	}
	title, hint := "daemon disconnected", "q quit"
	switch {
	case m.rc.upgraded && m.opts.CanRestart:
		title, hint = "agentws was upgraded", "r restart · q quit"
	case m.rc.upgraded:
		title, hint = "agentws was upgraded", "q quit, then run agentws again"
	case m.opts.Redial == nil:
	case m.rc.dialing || !m.opts.Now().Before(m.rc.at):
		hint = fmt.Sprintf("reconnecting · try %d… · q quit", m.rc.attempt)
	default:
		left := m.rc.at.Sub(m.opts.Now())
		secs := int((left + time.Second - 1) / time.Second)
		hint = fmt.Sprintf("reconnecting · try %d in %ds · q quit", m.rc.attempt, secs)
	}
	s := m.styles
	return []string{
		m.line(false, []piece{{s.need, " ● " + title}}, nil),
		m.line(false, []piece{{s.dim, " " + hint}}, nil),
	}
}

func Redialer(path string) func(context.Context) (Connection, error) {
	var mu sync.Mutex
	var prev []*rpc.Client
	return func(ctx context.Context) (Connection, error) {
		sub, err := rpc.Dial(path)
		if err != nil {
			return Connection{}, err
		}
		caller, err := rpc.Dial(path)
		if err != nil {
			_ = sub.Close()
			return Connection{}, err
		}
		s, err := sub.Subscribe(ctx)
		if err != nil {
			_ = sub.Close()
			_ = caller.Close()
			return Connection{}, err
		}
		mu.Lock()
		for _, c := range prev {
			_ = c.Close()
		}
		prev = []*rpc.Client{sub, caller}
		mu.Unlock()
		return Connection{State: s.State, Diffs: s.Diffs, Daemon: caller}, nil
	}
}
