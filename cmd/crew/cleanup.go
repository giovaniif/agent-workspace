package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const cleanupUsage = "usage: agentws cleanup [--dry-run]"

func runCleanup(args []string, stdout, stderr io.Writer) int {
	dryRun := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--dry-run":
		dryRun = true
	default:
		fmt.Fprintln(stderr, cleanupUsage)
		return 2
	}
	if err := cleanupCommand(dryRun, stdout); err != nil {
		fmt.Fprintf(stderr, "agentws cleanup: %v\n", err)
		return 1
	}
	return 0
}

func cleanupCommand(dryRun bool, stdout io.Writer) error {
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
	run := c.CleanupRun
	if dryRun {
		run = c.CleanupPlan
	}
	items, err := run(ctx)
	if err != nil {
		return err
	}
	printCleanup(stdout, items)
	return nil
}

func printCleanup(w io.Writer, items []rpc.CleanupItem) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	counts := map[domain.CleanupAction]int{}
	for _, it := range items {
		counts[it.Action]++
		line := fmt.Sprintf("%s\t%s\t%s\t%s", it.Action, it.Path, orDash(it.Branch), it.Reason)
		if it.Outcome != "" {
			line += "\t" + it.Outcome
		}
		fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "%d to remove, %d to back up and ask, %d kept\n",
		counts[domain.CleanupRemove], counts[domain.CleanupBackupThenAsk], counts[domain.CleanupKeep])
}
