package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
)

const setupUsage = "usage: agentws setup [codex|claude|omp|nvim [--remove]] | agentws setup bridge [--remote-bin path] [--remove] <ssh host>"

func runSetup(args []string, stdout, stderr io.Writer, env func(string) string, self string) int {
	if len(args) > 0 && args[0] == "nvim" {
		return runSetupNvim(args[1:], stdout, stderr, env, self)
	}
	if len(args) > 0 && args[0] == "bridge" {
		return runSetupBridge(args[1:], stdout, stderr, env, self)
	}
	if len(args) > 0 && args[0] == "omp" {
		return runSetupOmp(args[1:], stdout, stderr, env, self)
	}
	if len(args) > 0 && args[0] == "claude" {
		return runSetupClaude(args[1:], stdout, stderr, env, self)
	}
	if len(args) == 0 || args[0] != "codex" {
		fmt.Fprintln(stderr, setupUsage)
		return 2
	}
	fs := flag.NewFlagSet("setup codex", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remove := fs.Bool("remove", false, "undo the setup")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	dir, err := codexHome(env)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup codex: %v\n", err)
		return 1
	}
	cfg := codex.SetupConfig{Dir: dir, Command: self}
	if *remove {
		res, err := codex.Remove(cfg)
		return reportSetup(res, err, filepath.Join(dir, "hooks.json"), "removed the agentws hooks from", "nothing to remove in", "", stdout, stderr)
	}
	res, err := codex.Setup(cfg)
	return reportSetup(res, err, filepath.Join(dir, "hooks.json"), "merged the agentws hooks into", "already set up in", codex.TrustStep, stdout, stderr)
}

func reportSetup(res codex.SetupResult, err error, hooks, changed, unchanged, after string, stdout, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup codex: %v\n", err)
		return 1
	}
	if !res.Changed {
		fmt.Fprintf(stdout, "%s %s\n", unchanged, hooks)
		return 0
	}
	fmt.Fprintf(stdout, "%s %s\n", changed, hooks)
	if res.Backup != "" {
		fmt.Fprintf(stdout, "previous file saved as %s\n", res.Backup)
	}
	if after != "" {
		fmt.Fprintln(stdout, after)
	}
	return 0
}

func codexHome(env func(string) string) (string, error) {
	if dir := env("CODEX_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}
