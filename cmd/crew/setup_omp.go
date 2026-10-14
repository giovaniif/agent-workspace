package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/giovaniif/agent-workspace/internal/adapters/omp"
)

func runSetupOmp(args []string, stdout, stderr io.Writer, env func(string) string, self string) int {
	fs := flag.NewFlagSet("setup omp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remove := fs.Bool("remove", false, "undo the setup")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := omp.AgentDir(env)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup omp: %v\n", err)
		return 1
	}
	cfg := omp.SetupConfig{Dir: dir, Command: self}
	file := omp.HookFile(cfg.Dir)
	if *remove {
		res, err := omp.Remove(cfg)
		return reportOmpSetup(res.Changed, err, file, "removed the agentws hook file", "nothing to remove in", stdout, stderr)
	}
	res, err := omp.Setup(cfg)
	return reportOmpSetup(res.Changed, err, file, "wrote the agentws hook file", "already set up in", stdout, stderr)
}

func reportOmpSetup(changed bool, err error, file, did, same string, stdout, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup omp: %v\n", err)
		return 1
	}
	if changed {
		fmt.Fprintf(stdout, "%s %s\n", did, file)
		return 0
	}
	fmt.Fprintf(stdout, "%s %s\n", same, file)
	return 0
}
