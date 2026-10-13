package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const automationUsage = "usage: agentws automation list|show <name>"

func runAutomation(args []string, stdout, stderr io.Writer) int {
	wantArgs := map[string]int{"list": 1, "show": 2}
	if len(args) == 0 || len(args) != wantArgs[args[0]] {
		fmt.Fprintln(stderr, automationUsage)
		return 2
	}
	if err := automationCommand(args, stdout); err != nil {
		fmt.Fprintf(stderr, "agentws automation %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func automationCommand(args []string, stdout io.Writer) error {
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
	repo, _ := os.Getwd()
	if args[0] == "show" {
		var a rpc.Automation
		if err := c.Call(ctx, rpc.MethodAutomationShow, rpc.AutomationShowParams{Name: args[1], Repo: repo}, &a); err != nil {
			return err
		}
		printAutomation(stdout, a)
		return nil
	}
	var list rpc.AutomationList
	if err := c.Call(ctx, rpc.MethodAutomationList, rpc.AutomationListParams{}, &list); err != nil {
		return err
	}
	printAutomations(stdout, list)
	return nil
}

func automationTrigger(a domain.Automation) string {
	var parts []string
	if a.Schedule != "" {
		parts = append(parts, a.Schedule)
	}
	if len(a.On) > 0 {
		parts = append(parts, "on "+strings.Join(a.On, ","))
	}
	if len(parts) == 0 {
		return "manual"
	}
	return strings.Join(parts, "; ")
}

func automationTarget(a domain.Automation) string {
	if a.Target == "" {
		return domain.AutomationTargetNewWorktree
	}
	return a.Target
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func printAutomations(w io.Writer, list rpc.AutomationList) {
	if len(list.Automations) == 0 {
		fmt.Fprintln(w, "no automations. Add [[automation]] tables to $AGENTWS_HOME/config.toml or a repo's .agentws.toml.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAGENT\tTARGET\tTRIGGER\tENABLED\tSOURCE")
		for _, a := range list.Automations {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Name, a.Agent, automationTarget(a), automationTrigger(a), yesNo(a.Enabled), a.Source)
		}
		_ = tw.Flush()
	}
	for _, p := range list.Problems {
		fmt.Fprintf(w, "problem: %s\n", p)
	}
}

func printAutomation(w io.Writer, a domain.Automation) {
	row := func(k, v string) {
		if v != "" {
			fmt.Fprintf(w, "%-10s %s\n", k+":", v)
		}
	}
	row("name", a.Name)
	row("agent", a.Agent)
	row("model", a.Model)
	row("effort", a.Effort)
	row("target", automationTarget(a))
	row("trigger", automationTrigger(a))
	row("timezone", a.Timezone)
	row("precheck", a.Precheck)
	row("session", a.Session)
	row("enabled", yesNo(a.Enabled))
	row("source", a.Source)
	fmt.Fprintln(w, "prompt:")
	for _, line := range strings.Split(strings.TrimRight(a.Prompt, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}
