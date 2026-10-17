package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const mateUsage = "usage: crew mate list | show <name> | remove <name> | add <name> [--agent a] [--prompt p] [--no-open] | tokens <name> [--clear] | claim <name> | pause <name> | resume <name> | stop [--off] | set <name> [--agent a] [--prompt p] [--model m] [--effort e] [--repo r]... [--no-repos] [--owner id] [--enable|--disable]"

func runMate(args []string, stdout, stderr io.Writer) int {
	if !mateArgsOK(args) {
		fmt.Fprintln(stderr, mateUsage)
		return 2
	}
	if err := mateCommand(args, stdout); err != nil {
		fmt.Fprintf(stderr, "crew mate %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func mateArgsOK(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "list":
		return len(args) == 1
	case "show", "remove", "claim", "pause", "resume":
		return len(args) == 2
	case "stop":
		return len(args) == 1 || len(args) == 2 && args[1] == "--off"
	case "set":
		return len(args) >= 2
	case "add":
		_, _, err := parseMateAdd(args[1:])
		return err == nil
	case "tokens":
		return len(args) == 2 || len(args) == 3 && args[2] == "--clear"
	}
	return false
}

type repoList []string

func (r *repoList) String() string     { return strings.Join(*r, ",") }
func (r *repoList) Set(v string) error { *r = append(*r, v); return nil }

func mateFromFlags(base domain.Mate, args []string) (domain.Mate, error) {
	fs := flag.NewFlagSet("mate set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var repos repoList
	agent := fs.String("agent", "", "")
	prompt := fs.String("prompt", "", "")
	model := fs.String("model", "", "")
	effort := fs.String("effort", "", "")
	owner := fs.String("owner", "", "")
	fs.Var(&repos, "repo", "")
	noRepos := fs.Bool("no-repos", false, "")
	enable := fs.Bool("enable", false, "")
	disable := fs.Bool("disable", false, "")
	if err := fs.Parse(args); err != nil {
		return base, err
	}
	switch {
	case fs.NArg() > 0:
		return base, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	case *enable && *disable:
		return base, errors.New("--enable and --disable cannot go together")
	case *noRepos && len(repos) > 0:
		return base, errors.New("--repo and --no-repos cannot go together")
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "agent":
			base.Agent = *agent
		case "prompt":
			base.Prompt = *prompt
		case "model":
			base.Model = *model
		case "effort":
			base.Effort = *effort
		case "owner":
			base.Owner = *owner
		case "repo":
			base.Repos = []string(repos)
		case "no-repos":
			base.Repos = nil
		case "enable":
			base.Enabled = true
		case "disable":
			base.Enabled = false
		}
	})
	return base, nil
}

func mateCommand(args []string, stdout io.Writer) error {
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
	case "add":
		name, opts, err := parseMateAdd(args[1:])
		if err != nil {
			return err
		}
		return mateAdd(ctx, c, stdMateIO(stdout), name, opts)
	case "claim":
		return mateClaim(ctx, c, stdout, args[1])
	case "pause", "resume", "stop":
		return mateControl(ctx, c, stdout, args)
	case "tokens":
		return mateTokens(ctx, c, stdMateIO(stdout), args[1], len(args) == 3)
	case "show":
		var m rpc.Mate
		if err := c.Call(ctx, rpc.MethodMateShow, rpc.MateShowParams{Name: args[1]}, &m); err != nil {
			return err
		}
		printMate(stdout, m)
	case "remove":
		var m rpc.Mate
		if err := c.Call(ctx, rpc.MethodMateRemove, rpc.MateRemoveParams{Name: args[1]}, &m); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed %s\n", m.Name)
	case "set":
		base := domain.Mate{Name: args[1]}
		var existing rpc.Mate
		err := c.Call(ctx, rpc.MethodMateShow, rpc.MateShowParams{Name: args[1]}, &existing)
		var rerr *rpc.Error
		switch {
		case err == nil:
			base = existing
		case errors.As(err, &rerr) && rerr.Code == rpc.CodeNotFound:
			base.Enabled = true
		default:
			return err
		}
		m, err := mateFromFlags(base, args[2:])
		if err != nil {
			return err
		}
		var saved rpc.Mate
		if err := c.Call(ctx, rpc.MethodMateSet, rpc.MateSetParams{Mate: m}, &saved); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "saved %s\n", saved.Name)
	default:
		var list rpc.MateList
		if err := c.Call(ctx, rpc.MethodMateList, struct{}{}, &list); err != nil {
			return err
		}
		printMates(stdout, list)
	}
	return nil
}

func printMates(w io.Writer, list rpc.MateList) {
	if len(list.Mates) == 0 {
		fmt.Fprintln(w, "no mates. Run crew mate set <name> --agent claude --prompt \"...\", or add [[mate]] tables to $CREW_HOME/config.toml.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAGENT\tREPOS\tOWNER\tENABLED")
		for _, m := range list.Mates {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Name, m.Agent, dash(strings.Join(m.Repos, ",")), dash(m.Owner), yesNo(m.Enabled))
		}
		_ = tw.Flush()
	}
	for _, p := range list.Problems {
		fmt.Fprintf(w, "problem: %s\n", p)
	}
}

func printMate(w io.Writer, m domain.Mate) {
	row := func(k, v string) {
		if v != "" {
			fmt.Fprintf(w, "%-8s %s\n", k+":", v)
		}
	}
	row("name", m.Name)
	row("agent", m.Agent)
	row("model", m.Model)
	row("effort", m.Effort)
	row("repos", strings.Join(m.Repos, ","))
	row("owner", m.Owner)
	row("enabled", yesNo(m.Enabled))
	fmt.Fprintln(w, "prompt:")
	for _, line := range strings.Split(strings.TrimRight(m.Prompt, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

func mateControl(ctx context.Context, c mateCaller, out io.Writer, args []string) error {
	if args[0] == "stop" {
		stop := len(args) == 1
		if err := c.Call(ctx, rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: stop}, &domain.MateLimits{}); err != nil {
			return err
		}
		if stop {
			fmt.Fprintln(out, "stopped all mates; they start no new turns until crew mate stop --off")
		} else {
			fmt.Fprintln(out, "all mates can start turns again")
		}
		return nil
	}
	method, word := rpc.MethodMatePause, "paused"
	if args[0] == "resume" {
		method, word = rpc.MethodMateResume, "resumed"
	}
	var m domain.Mate
	if err := c.Call(ctx, method, rpc.MateNameParams{Name: args[1]}, &m); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s\n", word, args[1])
	return nil
}
