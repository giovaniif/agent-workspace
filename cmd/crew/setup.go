package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/giovaniif/crew/internal/adapters/codex"
)

const setupUsage = "usage: crew setup [codex|claude|omp|nvim [--remove]] | crew setup bridge [--remote-bin path] [--remove] <ssh host> | crew setup serve [--remove] [--addr host:port] [--cert file --key file | --self-signed] [--url https://host] | crew setup daemon [--remove | --check]"

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
	if len(args) > 0 && args[0] == "serve" {
		return runSetupServe(args[1:], stdout, stderr, env, self, runtime.GOOS)
	}
	if len(args) > 0 && args[0] == "daemon" {
		return runSetupDaemon(args[1:], stdout, stderr, env, self, runtime.GOOS)
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
		fmt.Fprintf(stderr, "crew setup codex: %v\n", err)
		return 1
	}
	cfg := codex.SetupConfig{Dir: dir, Command: self}
	if *remove {
		res, err := codex.Remove(cfg)
		code := reportSetup(res, err, filepath.Join(dir, "hooks.json"), "removed the crew hooks from", "nothing to remove in", "", stdout, stderr)
		return skillStep(code, removeSkill(dir, stdout), stderr)
	}
	res, err := codex.Setup(cfg)
	code := reportSetup(res, err, filepath.Join(dir, "hooks.json"), "merged the crew hooks into", "already set up in", codex.TrustStep, stdout, stderr)
	return skillStep(code, installSkill(dir, stdout), stderr)
}

func skillStep(code int, err error, stderr io.Writer) int {
	if code != 0 {
		return code
	}
	if err != nil {
		fmt.Fprintf(stderr, "crew setup codex: %v\n", err)
		return 1
	}
	return 0
}

func reportSetup(res codex.SetupResult, err error, hooks, changed, unchanged, after string, stdout, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "crew setup codex: %v\n", err)
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
