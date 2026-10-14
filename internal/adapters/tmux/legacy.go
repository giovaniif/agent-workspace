package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/giovaniif/crew/internal/domain"
)

type names struct {
	session      string
	option       string
	titleOption  string
	popupPrefix  string
	nativePrefix string
}

func namesFor(socket string) names {
	if socket == domain.LegacyTmuxServer {
		return names{session: "agentws", option: "@agentws", titleOption: "@agentws_title", popupPrefix: "agentws-popup-", nativePrefix: "agentws-native-"}
	}
	return names{session: "crew", option: "@crew", titleOption: "@crew_title", popupPrefix: "crew-popup-", nativePrefix: "crew-native-"}
}

func HasAgentPanes(ctx context.Context, socket string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "-L", socket, "list-windows", "-t", "="+namesFor(socket).session, "-F", "#{window_name}")
	cmd.Env = withoutTmuxEnv(os.Environ())
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
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
