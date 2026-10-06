package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const TickInterval = 200 * time.Millisecond

const FPS = 120

func Run(ctx context.Context, subscriber, caller *rpc.Client, opts Options) error {
	sub, err := subscriber.Subscribe(ctx)
	if err != nil {
		return err
	}
	opts.Tick = TickInterval
	opts.Focus, opts.Attend, opts.Kill, opts.Calls = caller, caller, caller, caller
	opts.Switch, opts.Review, opts.Disk = caller, caller, caller
	opts.Onboard = caller
	m := New(opts)
	next, first := m.Update(StateMsg(sub.State))
	start := next.(Model)
	start.startup = tea.Batch(first, listen(sub.Diffs))
	p := tea.NewProgram(start, tea.WithContext(ctx), tea.WithFPS(FPS))
	final, err := p.Run()
	if f, ok := final.(Model); ok && err == nil && f.restart {
		return ErrRestart
	}
	return err
}
