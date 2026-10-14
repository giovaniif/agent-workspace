package main

import (
	"context"
	"fmt"
	"io"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func runFocus(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: agentws focus <session id>")
		return 2
	}
	home, err := rpc.Home()
	if err == nil {
		ctx := context.Background()
		var c *rpc.Client
		if c, err = connect(ctx, home); err == nil {
			err = c.FocusSession(ctx, args[0])
			_ = c.Close()
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentws focus %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func openClientParams(self, home, dir string, getenv func(string) string) rpc.OpenClientParams {
	return rpc.OpenClientParams{
		Command:  []string{self, "tui"},
		Env:      map[string]string{domain.EnvName("HOME"): home, domain.EnvName("LAUNCH_DIR"): dir},
		Dir:      dir,
		Terminal: domain.TerminalBundle(getenv("__CFBundleIdentifier"), getenv("TERM_PROGRAM")),
	}
}
