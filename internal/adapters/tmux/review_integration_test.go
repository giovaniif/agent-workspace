//go:build integration

package tmux_test

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
)

func TestReviewLayoutWidensTheSidebarAndKeepsItWideOnResize(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	attach := h.AttachCommand(slot)
	tmuxArgs := func(args ...string) []string {
		return append(slices.Clone(attach[1:len(attach)-3]), args...)
	}
	tmuxRun := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(attach[0], tmuxArgs(args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	width := func() int {
		n, _ := strconv.Atoi(tmuxRun("display-message", "-p", "-t", string(slot)+".0", "#{pane_width}"))
		return n
	}
	tmuxRun("resize-window", "-t", string(slot), "-x", "200", "-y", "50")
	waitFor(t, "sidebar back to its width", func() bool { return width() == tmux.SidebarWidth })

	if err := h.WidenSidebar(ctx, slot, true); err != nil {
		t.Fatal(err)
	}
	if w := width(); w < 140 {
		t.Fatalf("widened sidebar is %d of 200 columns", w)
	}
	tmuxRun("resize-window", "-t", string(slot), "-x", "160")
	waitFor(t, "sidebar to stay wide after a resize", func() bool { return width() >= 110 })

	if err := h.WidenSidebar(ctx, slot, false); err != nil {
		t.Fatal(err)
	}
	if w := width(); w != tmux.SidebarWidth {
		t.Fatalf("narrowed sidebar is %d, want %d", w, tmux.SidebarWidth)
	}
	tmuxRun("resize-window", "-t", string(slot), "-x", "190")
	waitFor(t, "sidebar to stay narrow after a resize", func() bool { return width() == tmux.SidebarWidth })
}
