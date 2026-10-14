package codex

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

const maxAncestors = 64

func ResolvePane(env func(string) string, pid int, parentOf func(int) (int, bool), panePIDs map[int]app.PaneID) (app.PaneID, bool) {
	if pane := env("TMUX_PANE"); pane != "" {
		return app.PaneID(pane), true
	}
	seen := map[int]bool{}
	for i := 0; i < maxAncestors && pid > 1 && !seen[pid]; i++ {
		if pane, ok := panePIDs[pid]; ok {
			return pane, true
		}
		seen[pid] = true
		next, ok := parentOf(pid)
		if !ok {
			return "", false
		}
		pid = next
	}
	return "", false
}

func ProcessParent(pid int) (int, bool) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return ppid, true
}
