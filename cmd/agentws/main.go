package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/version"
)

var stubs []string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return attach(stderr)
	}
	switch cmd := args[0]; {
	case cmd == "tui":
		return runTUI(args[1:], stderr)
	case cmd == "debug":
		return runDebug(args[1:], stdout, stderr)
	case cmd == "version" && len(args) == 2 && args[1] == "--build":
		fmt.Fprintln(stdout, version.String())
		return 0
	case cmd == "version":
		return runVersion(version.Version, buildCommit(), latestRelease, stdout)
	case cmd == "hook":
		home, _ := rpc.Home()
		return runHook(args[1:], os.Stdin, stdout, home, os.Getenv("TMUX_PANE"))
	case cmd == "setup" && len(args) == 1:
		return runTUI([]string{"--setup"}, stderr)
	case cmd == "setup":
		self, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "agentws setup: %v\n", err)
			return 1
		}
		return runSetup(args[1:], stdout, stderr, os.Getenv, self)
	case cmd == "statusline":
		home, _ := rpc.Home()
		return runStatusLine(args[1:], os.Stdin, stdout, home, os.Getenv("TMUX_PANE"))
	case cmd == "daemon":
		return runDaemon(args[1:], stdout, stderr)
	case cmd == "setup-worktree":
		return runSetupWorktree(args[1:], stdout, stderr)
	case cmd == "workspace":
		return runWorkspace(args[1:], stdout, stderr)
	case cmd == "cleanup":
		return runCleanup(args[1:], stdout, stderr)
	case cmd == "worktree":
		return runWorktree(args[1:], stdout, stderr)
	case cmd == "pr":
		return runPR(args[1:], stdout, stderr)
	case cmd == "new":
		return runNew(args[1:], stdout, stderr)
	case cmd == "review":
		return runReview(args[1:], stdout, stderr)
	case cmd == "notify":
		return runNotify(args[1:], stdout, stderr)
	case cmd == "focus":
		return runFocus(args[1:], stderr)
	case cmd == "remote":
		return runRemote(args[1:], stdout, stderr)
	case cmd == "serve":
		return runServe(args[1:], stdout, stderr)
	case cmd == "rpc":
		return runRPC(args[1:], os.Stdin, stdout, stderr)
	case slices.Contains(stubs, cmd):
		fmt.Fprintf(stderr, "agentws %s: not implemented yet\n", cmd)
		return 1
	default:
		fmt.Fprintf(stderr, "agentws: unknown command %q\n", cmd)
		usage(stderr)
		return 2
	}
}

func buildCommit() string {
	if version.Commit != "" {
		return version.Commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "unknown"
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: agentws [daemon|workspace|worktree|pr|setup|setup-worktree|tui|debug|hook|statusline|new|review|focus|notify|remote|serve|rpc|cleanup|version [--build]]")
}
