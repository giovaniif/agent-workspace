package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const approvalUsage = "usage: crew approval list | approve <id> | deny <id>"

func runApproval(args []string, stdout, stderr io.Writer) int {
	ok := len(args) == 1 && args[0] == "list" || len(args) == 2 && (args[0] == "approve" || args[0] == "deny")
	if !ok {
		fmt.Fprintln(stderr, approvalUsage)
		return 2
	}
	if err := approvalCommand(args, stdout); err != nil {
		fmt.Fprintf(stderr, "crew approval %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func approvalCommand(args []string, stdout io.Writer) error {
	home, err := rpc.Home()
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := connect(ctx, home)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if args[0] == "list" {
		var list []domain.Approval
		if err := c.Call(ctx, rpc.MethodApprovalList, struct{}{}, &list); err != nil {
			return err
		}
		printApprovals(stdout, list, time.Now())
		return nil
	}
	var a domain.Approval
	if err := c.Call(ctx, rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: args[1], Approve: args[0] == "approve"}, &a); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s\n", a.ID, a.State)
	return nil
}

func printApprovals(w io.Writer, list []domain.Approval, now time.Time) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no pending approvals")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tMATE\tACTION\tBY\tEXPIRES\tINPUT")
	for _, a := range list {
		left := a.At.Add(domain.ApprovalTTL).Sub(now).Truncate(time.Minute)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, dash(a.Mate), strings.ReplaceAll(string(a.Action.Kind), "_", " "), dash(a.Requester), left, a.Action.Input)
	}
	_ = tw.Flush()
}
