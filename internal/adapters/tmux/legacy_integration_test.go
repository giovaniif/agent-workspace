//go:build integration

package tmux_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/tmux"
)

func TestHasAgentPanesIsTrueOnlyWhileTheServerParksAnAgentPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx := context.Background()
	socket := fmt.Sprintf("crew-legacy-%d-%d", os.Getpid(), time.Now().UnixNano())
	if tmux.HasAgentPanes(ctx, socket) {
		t.Fatal("no server, but panes reported")
	}
	h := tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(t.TempDir(), "tmux.conf")})
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	pane, err := h.Create(ctx, catPane("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !tmux.HasAgentPanes(ctx, socket) {
		t.Fatal("an agent pane is parked, but none reported")
	}
	if err := h.Kill(ctx, pane); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "no agent panes", func() bool { return !tmux.HasAgentPanes(ctx, socket) })
}
