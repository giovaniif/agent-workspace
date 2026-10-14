package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const automationUsage = "usage: crew automation list | show <name> | run [--work-item w] <name> [context...|-] | runs [name] | rerun <run id> | trust <name>"

func runAutomation(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || !automationArgsOK(args) {
		fmt.Fprintln(stderr, automationUsage)
		return 2
	}
	if err := automationCommand(args, os.Stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "crew automation %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func automationArgsOK(args []string) bool {
	switch args[0] {
	case "list":
		return len(args) == 1
	case "show", "rerun", "trust":
		return len(args) == 2
	case "runs":
		return len(args) <= 2
	case "run":
		return len(args) >= 2
	}
	return false
}

func automationRunParams(args []string, stdin io.Reader) (rpc.AutomationRunParams, error) {
	var p rpc.AutomationRunParams
	if len(args) >= 2 && args[0] == "--work-item" {
		p.WorkItem, args = args[1], args[2:]
	}
	if len(args) == 0 {
		return p, fmt.Errorf("%s", automationUsage)
	}
	p.Name = args[0]
	rest := args[1:]
	if len(rest) == 1 && rest[0] == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return p, err
		}
		p.Context = strings.TrimSpace(string(data))
	} else {
		p.Context = strings.Join(rest, " ")
	}
	p.Repo, _ = os.Getwd()
	return p, nil
}

func automationCommand(args []string, stdin io.Reader, stdout io.Writer) error {
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
	switch args[0] {
	case "run", "rerun":
		p := rpc.AutomationRunParams{Rerun: args[len(args)-1]}
		if args[0] == "run" {
			var err error
			if p, err = automationRunParams(args[1:], stdin); err != nil {
				return err
			}
		}
		var got rpc.AutomationRunResult
		if err := c.Call(ctx, rpc.MethodAutomationRun, p, &got); err != nil {
			return err
		}
		printAutomationRuns(stdout, []domain.AutomationRun{got.Run})
		return nil
	case "trust":
		var a rpc.Automation
		if err := c.Call(ctx, rpc.MethodAutomationTrust, rpc.AutomationShowParams{Name: args[1], Repo: cwd()}, &a); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "trusted %s from %s\n", a.Name, a.Source)
		return nil
	case "runs":
		p := rpc.AutomationRunsParams{Limit: 50}
		if len(args) == 2 {
			p.Name = args[1]
		}
		var got rpc.AutomationRuns
		if err := c.Call(ctx, rpc.MethodAutomationRuns, p, &got); err != nil {
			return err
		}
		printAutomationRuns(stdout, got.Runs)
		return nil
	}
	if args[0] == "show" {
		var a rpc.Automation
		if err := c.Call(ctx, rpc.MethodAutomationShow, rpc.AutomationShowParams{Name: args[1]}, &a); err != nil {
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
		fmt.Fprintln(w, "no automations. Add [[automation]] tables to $CREW_HOME/config.toml or a repo's .agentws.toml.")
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

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printAutomationRuns(w io.Writer, runs []domain.AutomationRun) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAUTOMATION\tTRIGGER\tSTART\tOUTCOME\tSESSION\tCONTEXT")
	for _, r := range runs {
		note := domain.OneLine(r.Context, 60)
		switch {
		case r.Reason != "" && note == "":
			note = r.Reason
		case r.Reason != "":
			note += " (" + r.Reason + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Automation, r.Trigger, r.Start.Local().Format("2006-01-02 15:04"), r.Outcome, dash(r.SessionID), dash(note))
	}
	_ = tw.Flush()
}

func cwd() string {
	dir, _ := os.Getwd()
	return dir
}
