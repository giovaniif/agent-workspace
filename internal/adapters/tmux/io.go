package tmux

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

func (h *Host) SendText(ctx context.Context, pane app.PaneID, text string, bracketedPaste bool) error {
	buffer := fmt.Sprintf("crew-%d-%d", os.Getpid(), h.bufferSeq.Add(1))
	if _, err := h.run(ctx, text, "load-buffer", "-b", buffer, "-"); err != nil {
		return err
	}
	args := []string{"paste-buffer", "-d", "-b", buffer, "-t", string(pane)}
	if bracketedPaste {
		args = append(args, "-p", "-r")
	}
	_, err := h.run(ctx, "", args...)
	return err
}

func (h *Host) SendKeys(ctx context.Context, pane app.PaneID, keys ...string) error {
	_, err := h.run(ctx, "", append([]string{"send-keys", "-t", string(pane)}, keys...)...)
	return err
}

func (h *Host) Capture(ctx context.Context, pane app.PaneID, lines int) (string, error) {
	out, err := h.run(ctx, "", "capture-pane", "-p", "-J", "-t", string(pane), "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", err
	}
	all := strings.Split(trimCapture(out), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}

func trimCapture(s string) string {
	rows := strings.Split(s, "\n")
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return strings.Join(rows, "\n")
}
