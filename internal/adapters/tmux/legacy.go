package tmux

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

func HasAgentPanes(ctx context.Context, socket string) bool {
	cmd := exec.CommandContext(ctx, "tmux", "-L", socket, "list-windows", "-t", "="+sessionName, "-F", "#{window_name}")
	cmd.Env = withoutTmuxEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, name := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(name, "pane-") {
			return true
		}
	}
	return false
}
