package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func HasAgentPanes(ctx context.Context, socket string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "-L", socket, "list-windows", "-t", "="+sessionName, "-F", "#{window_name}")
	cmd.Env = withoutTmuxEnv(os.Environ())
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && noServer(stderr.String()) {
			return false, nil
		}
		return false, fmt.Errorf("tmux -L %s list-windows: %w: %s", socket, err, strings.TrimSpace(stderr.String()))
	}
	for _, name := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(name, "pane-") {
			return true, nil
		}
	}
	return false, nil
}

func noServer(stderr string) bool {
	for _, s := range []string{"no server running", "error connecting", "can't find session", "no such file or directory"} {
		if strings.Contains(stderr, s) {
			return true
		}
	}
	return false
}
