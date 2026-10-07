//go:build integration

package tmux_test

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/tmux"
	"github.com/giovaniif/agent-workspace/internal/app"
)

func TestClientLayoutCanBeFoundFocusedAndAttached(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	if !h.ClientOpen(ctx, slot) {
		t.Fatal("ClientOpen is false for a layout just opened")
	}
	if h.ClientOpen(ctx, "@9999") {
		t.Fatal("ClientOpen is true for a window that does not exist")
	}

	if err := h.FocusSlot(ctx, slot); err != nil {
		t.Fatal(err)
	}
	attach := h.AttachCommand(slot)
	if attach[0] != "tmux" || !slices.Contains(attach, "-L") || !slices.Contains(attach, "=agentws:"+string(slot)) {
		t.Fatalf("attach = %v; want tmux on the dedicated socket targeting %s", attach, slot)
	}
	active := slices.Clone(attach[:len(attach)-3])
	active = append(active, "display-message", "-p", "-t", string(slot), "#{pane_index}")
	out, err := exec.Command(active[0], active[1:]...).Output()
	if err != nil {
		t.Fatalf("%v: %v", active, err)
	}
	if got := strings.TrimSpace(string(out)); got != "1" {
		t.Fatalf("active pane index = %q after FocusSlot; want the main slot, 1", got)
	}
	width := append(slices.Clone(attach[:len(attach)-3]), "display-message", "-p", "-t", string(slot)+".0", "#{pane_width}")
	out, err = exec.Command(width[0], width[1:]...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != strconv.Itoa(tmux.SidebarWidth) {
		t.Fatalf("sidebar pane is %s columns wide, want %d", got, tmux.SidebarWidth)
	}
	resize := append(slices.Clone(attach[:len(attach)-3]), "resize-window", "-t", string(slot), "-x", "130")
	if out, err := exec.Command(resize[0], resize[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	waitFor(t, "sidebar to keep its width after a resize", func() bool {
		out, err := exec.Command(width[0], width[1:]...).Output()
		return err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(tmux.SidebarWidth)
	})

	if _, err := exec.Command(active[0], append(slices.Clone(attach[1:len(attach)-3]), "kill-window", "-t", string(slot))...).CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if h.ClientOpen(ctx, slot) {
		t.Fatal("ClientOpen is true after the window was killed")
	}
}
