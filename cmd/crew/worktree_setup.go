package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/git"
	"github.com/giovaniif/agent-workspace/internal/adapters/setup"
	"github.com/giovaniif/agent-workspace/internal/app"
)

const worktreeSetupUsage = "usage: agentws setup-worktree <path>"

func runSetupWorktree(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, worktreeSetupUsage)
		return 2
	}
	worktree, err := resolveDir(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup-worktree: %v\n", err)
		return 1
	}
	service := app.WorktreeSetup{
		Recipes: setup.Recipes{},
		FS:      setup.FS{},
		Runner:  setup.Shell{Out: stderr},
		Git:     git.Worktrees{},
		Now:     time.Now,
	}
	report, err := service.Run(context.Background(), worktree)
	for _, line := range report.Lines {
		fmt.Fprintln(stdout, line)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup-worktree: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, setupSummary(report))
	return 0
}

func resolveDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func setupSummary(r app.SetupReport) string {
	return fmt.Sprintf("done in %s, disk used %s", r.Duration.Round(time.Millisecond), formatBytes(r.DiskUsed))
}

func formatBytes(n int64) string {
	abs := float64(n)
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}
