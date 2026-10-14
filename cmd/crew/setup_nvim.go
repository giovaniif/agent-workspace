package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/giovaniif/agent-workspace/internal/adapters/onboard"
)

func runSetupNvim(args []string, stdout, stderr io.Writer, env func(string) string, self string) int {
	fs := flag.NewFlagSet("setup nvim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remove := fs.Bool("remove", false, "undo the setup")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p := onboard.FromEnv("", self, env)
	ctx := context.Background()
	if *remove {
		removed, err := p.RemoveNvim(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "agentws setup nvim: %v\n", err)
			return 1
		}
		if removed {
			fmt.Fprintln(stdout, "removed the agentws setup file from the nvim config")
		} else {
			fmt.Fprintln(stdout, "no agentws setup file in the nvim config")
		}
		return 0
	}
	n, err := p.InstallNvim(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup nvim: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s, which nvim loads at startup; agentws setup nvim --remove deletes it\n", n.ConfigFile)
	return 0
}
