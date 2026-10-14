package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const debugSessionUsage = "usage: agentws debug session [--once] <id>"

func runDebugSession(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("debug session", flag.ContinueOnError)
	fs.SetOutput(stderr)
	once := fs.Bool("once", false, "print the current state and exit")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(stderr, debugSessionUsage)
		return 2
	}
	home, err := rpc.Home()
	if err != nil {
		fmt.Fprintf(stderr, "agentws debug: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := connect(ctx, home)
	if err == nil {
		defer func() { _ = c.Close() }()
		var sub rpc.Subscription
		if sub, err = c.Subscribe(ctx); err == nil {
			err = streamSession(ctx, sub, fs.Arg(0), !*once, stdout)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentws debug: %v\n", err)
		return 1
	}
	return 0
}

func streamSession(ctx context.Context, sub rpc.Subscription, id string, follow bool, w io.Writer) error {
	var current *domain.Session
	for i := range sub.State.Sessions {
		if sub.State.Sessions[i].ID == id {
			current = &sub.State.Sessions[i]
		}
	}
	if current == nil {
		return errors.New("no session " + id)
	}
	fmt.Fprintln(w, describeSession(sub.State.Seq, *current))
	if !follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case diff, ok := <-sub.Diffs:
			if !ok {
				return nil
			}
			if diff.Session != nil && diff.Session.ID == id {
				fmt.Fprintln(w, describeSession(diff.Seq, *diff.Session))
			}
		}
	}
}

func describeSession(seq uint64, s domain.Session) string {
	return fmt.Sprintf("seq=%d state=%s harness=%s pane=%s model=%s effort=%s context_left=%d%% limit_used=%d%%",
		seq, s.State, s.Harness, s.Pane, s.Model, s.Effort, s.Usage.ContextLeftPercent, s.Usage.LimitUsedPercent)
}
