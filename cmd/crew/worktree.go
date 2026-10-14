package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const worktreeUsage = "usage: agentws worktree <list|assign <path> <session>>"

func runWorktree(args []string, stdout, stderr io.Writer) int {
	wantArgs := map[string]int{"list": 1, "assign": 3}
	if len(args) == 0 || len(args) != wantArgs[args[0]] {
		fmt.Fprintln(stderr, worktreeUsage)
		return 2
	}
	if err := worktreeCommand(args, stdout); err != nil {
		fmt.Fprintf(stderr, "agentws worktree: %v\n", err)
		return 1
	}
	return 0
}

func worktreeCommand(args []string, stdout io.Writer) error {
	home, err := rpc.Home()
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := rpc.Connect(ctx, rpc.SocketPath(home), func() error { return spawn(home) })
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if args[0] == "assign" {
		path, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		if err := c.WorktreeAssign(ctx, path, args[2]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "assigned %s to %s\n", path, args[2])
		return nil
	}
	sub, err := c.Subscribe(ctx)
	if err != nil {
		return err
	}
	printWorktrees(stdout, sub.State.Worktrees)
	return nil
}

func printWorktrees(w io.Writer, wts []domain.Worktree) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, wt := range wts {
		owner := wt.SessionID
		if owner == "" {
			owner = "unassigned"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", wt.Path, orDash(wt.Branch), owner, prLabel(wt.PR))
	}
	_ = tw.Flush()
}

func prLabel(pr *domain.PullRequest) string {
	if pr == nil {
		return "-"
	}
	label := fmt.Sprintf("#%d %s", pr.Number, pr.State)
	if pr.Checks != domain.CheckNone {
		label += " " + string(pr.Checks)
	}
	return label
}
