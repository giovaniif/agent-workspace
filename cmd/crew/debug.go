package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const debugUsage = "usage: agentws debug seed <count> [--codex]\n       agentws debug launch --harness claude --dir <dir> [--model m] [--effort e] [--name n]\n       agentws debug session [--once] <id>"

func runDebug(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, debugUsage)
		return 2
	}
	var err error
	switch args[0] {
	case "seed":
		return runDebugSeed(args, stdout, stderr)
	case "launch":
		fs := flag.NewFlagSet("debug launch", flag.ContinueOnError)
		fs.SetOutput(stderr)
		var p rpc.LaunchParams
		fs.StringVar(&p.Harness, "harness", "claude", "claude, codex or omp")
		fs.StringVar(&p.Dir, "dir", ".", "the session's working directory")
		fs.StringVar(&p.Model, "model", "", "model")
		fs.StringVar(&p.Effort, "effort", "", "effort")
		fs.StringVar(&p.Name, "name", "", "pane name")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		err = debugLaunch(p, stdout)
	case "session":
		return runDebugSession(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, debugUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentws debug: %v\n", err)
		return 1
	}
	return 0
}

func connectHome(ctx context.Context) (*rpc.Client, error) {
	home, err := rpc.Home()
	if err != nil {
		return nil, err
	}
	return connect(ctx, home)
}

func debugLaunch(p rpc.LaunchParams, stdout io.Writer) error {
	ctx := context.Background()
	c, err := connectHome(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var s domain.Session
	if err := c.Call(ctx, rpc.MethodLaunch, p, &s); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "session %s on pane %s\n", s.ID, s.Pane)
	return nil
}
