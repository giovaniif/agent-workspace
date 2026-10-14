package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const workspaceUsage = "usage: agentws workspace <add|list|remove> [path]"

func runWorkspace(args []string, stdout, stderr io.Writer) int {
	wantArgs := map[string]int{"add": 2, "remove": 2, "list": 1}
	if len(args) == 0 || len(args) != wantArgs[args[0]] {
		fmt.Fprintln(stderr, workspaceUsage)
		return 2
	}
	sub := args[0]
	if err := workspaceCommand(sub, args[1:], stdout); err != nil {
		fmt.Fprintf(stderr, "agentws workspace: %v\n", err)
		return 1
	}
	return 0
}

func workspaceCommand(sub string, args []string, stdout io.Writer) error {
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

	switch sub {
	case "add":
		root, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		ws, err := c.WorkspaceAdd(ctx, root)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "added %s (%s, %s)\n", ws.Root, ws.Kind, repoCount(len(ws.Repos)))
	case "remove":
		root, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		if err := c.WorkspaceRemove(ctx, root); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed %s\n", root)
	default:
		list, err := c.WorkspaceList(ctx)
		if err != nil {
			return err
		}
		printWorkspaces(stdout, list)
	}
	return nil
}

func repoCount(n int) string {
	if n == 1 {
		return "1 repo"
	}
	return fmt.Sprintf("%d repos", n)
}

func printWorkspaces(w io.Writer, list rpc.WorkspaceList) {
	for _, ws := range list.Workspaces {
		mark := string(ws.Kind)
		if ws.Root == list.LastUsed {
			mark += ", last used"
		}
		fmt.Fprintf(w, "%s (%s)\n", ws.Root, mark)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, r := range ws.Repos {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", r.Name, orDash(r.Branch), orDash(r.DefaultBranch), changed(r))
		}
		_ = tw.Flush()
	}
}

func changed(r domain.Repo) string {
	switch {
	case r.Branch == "":
		return "-"
	case r.ChangedFiles == 0:
		return "clean"
	default:
		return fmt.Sprintf("%d changed", r.ChangedFiles)
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
