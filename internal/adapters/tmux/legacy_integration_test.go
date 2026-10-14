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
	if has, err := tmux.HasAgentPanes(ctx, socket); has || err != nil {
		t.Fatalf("no server: %v, %v", has, err)
	}
	h := tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(t.TempDir(), "tmux.conf")})
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	pane, err := h.Create(ctx, catPane("a"))
	if err != nil {
		t.Fatal(err)
	}
	if has, err := tmux.HasAgentPanes(ctx, socket); !has || err != nil {
		t.Fatalf("an agent pane is parked: %v, %v", has, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tmux.HasAgentPanes(cancelled, socket); err == nil {
		t.Fatal("a failed probe of a live server reads as no panes")
	}
	if err := h.Kill(ctx, pane); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "no agent panes", func() bool {
		has, err := tmux.HasAgentPanes(ctx, socket)
		return !has && err == nil
	})
}
