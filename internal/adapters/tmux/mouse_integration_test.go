//go:build integration

package tmux_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
)

func mouseHost(t *testing.T, noMouse bool) (*tmux.Host, app.Slot) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	h := tmux.New(tmux.Config{
		Socket:     fmt.Sprintf("agentws-test-mouse-%d-%d", os.Getpid(), time.Now().UnixNano()),
		ConfigPath: filepath.Join(t.TempDir(), "tmux.conf"),
		NoMouse:    noMouse,
	})
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	slot, err := h.OpenClient(context.Background(), "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	return h, slot
}

func TestMouseOnBindsClickDragAndWheel(t *testing.T) {
	h, slot := mouseHost(t, false)
	if got, _ := h.ShowOption(context.Background(), "mouse"); got != "on" {
		t.Fatalf("mouse = %q, want on", got)
	}
	root := tmuxIn(t, h, slot, "list-keys", "-T", "root")
	for _, k := range []string{"MouseDown1Pane", "MouseDrag1Border", "WheelUpPane", "MouseDrag1Pane"} {
		if !strings.Contains(root, k) {
			t.Errorf("root table lacks %s:\n%s", k, root)
		}
	}
	if !strings.Contains(root, `select-pane -t = \; send-keys -M`) {
		t.Errorf("a click does not focus the pane and pass the click on:\n%s", root)
	}
	if !strings.Contains(root, "mouse_any_flag") {
		t.Errorf("the wheel is not passed to programs that take the mouse:\n%s", root)
	}
	copyMode := tmuxIn(t, h, slot, "list-keys", "-T", "copy-mode")
	for _, k := range []string{"WheelUpPane", "WheelDownPane", "MouseDragEnd1Pane"} {
		if !strings.Contains(copyMode, k) {
			t.Errorf("copy-mode table lacks %s:\n%s", k, copyMode)
		}
	}
}

func TestMouseOffLeavesTheMouseToTheTerminal(t *testing.T) {
	h, _ := mouseHost(t, true)
	if got, _ := h.ShowOption(context.Background(), "mouse"); got != "off" {
		t.Fatalf("mouse = %q, want off", got)
	}
}

func TestADaemonRestartLoadsTheNewConfigIntoARunningServer(t *testing.T) {
	ctx := context.Background()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := tmux.Config{
		Socket:     fmt.Sprintf("agentws-test-mouse-%d-%d", os.Getpid(), time.Now().UnixNano()),
		ConfigPath: filepath.Join(t.TempDir(), "tmux.conf"),
		NoMouse:    true,
	}
	old := tmux.New(cfg)
	t.Cleanup(func() { _ = old.Close(context.Background()) })
	if _, err := old.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	cfg.NoMouse = false
	restarted := tmux.New(cfg)
	if _, err := restarted.Create(ctx, app.PaneSpec{Name: "agent", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := restarted.ShowOption(ctx, "mouse"); got != "on" {
		t.Fatalf("mouse = %q after the restart, want on", got)
	}
}

func TestACanceledFirstCommandStillLeavesTheConfigToLoad(t *testing.T) {
	ctx := context.Background()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := tmux.Config{
		Socket:     fmt.Sprintf("agentws-test-mouse-%d-%d", os.Getpid(), time.Now().UnixNano()),
		ConfigPath: filepath.Join(t.TempDir(), "tmux.conf"),
		NoMouse:    true,
	}
	old := tmux.New(cfg)
	t.Cleanup(func() { _ = old.Close(context.Background()) })
	if _, err := old.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	cfg.NoMouse = false
	restarted := tmux.New(cfg)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = restarted.ShowOption(canceled, "mouse")
	if got, _ := restarted.ShowOption(ctx, "mouse"); got != "on" {
		t.Fatalf("mouse = %q after a canceled first command, want on", got)
	}
}
