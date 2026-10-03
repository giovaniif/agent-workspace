package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const newUsage = "usage: agentws new [--workspace path] [--harness claude|codex|omp] [--model m] [--effort e] <work item>"

var errNoWorkItem = errors.New("no work item")

// why: an empty workspace means the last used one.
func parseNewArgs(args []string, stderr io.Writer) (rpc.NewSessionParams, error) {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var p rpc.NewSessionParams
	fs.StringVar(&p.Workspace, "workspace", "", "workspace root (default: last used)")
	fs.StringVar(&p.Harness, "harness", string(domain.HarnessClaude), "claude, codex or omp")
	fs.StringVar(&p.Model, "model", "", "model (default: the harness default)")
	fs.StringVar(&p.Effort, "effort", "", "effort (default: the harness default)")
	if err := fs.Parse(args); err != nil {
		return p, err
	}
	p.WorkItem = strings.TrimSpace(strings.Join(fs.Args(), " "))
	if p.WorkItem == "" {
		return p, errNoWorkItem
	}
	if p.Workspace != "" {
		abs, err := filepath.Abs(p.Workspace)
		if err != nil {
			return p, err
		}
		p.Workspace = abs
	}
	return p, nil
}

func runNew(args []string, stdout, stderr io.Writer) int {
	p, err := parseNewArgs(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, newUsage)
		return 2
	}
	ctx := context.Background()
	c, err := connectHome(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "agentws new: %v\n", err)
		return 1
	}
	defer func() { _ = c.Close() }()
	var s domain.Session
	if err := c.Call(ctx, rpc.MethodNewSession, p, &s); err != nil {
		fmt.Fprintf(stderr, "agentws new: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "session %s on pane %s\n", s.ID, s.Pane)
	return 0
}
