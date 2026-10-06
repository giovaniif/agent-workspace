//go:build integration

package tmux_test

import (
	"context"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/tmux"
	"github.com/giovaniif/agent-workspace/internal/app"
)

var _ app.TerminalActivity = (*tmux.Host)(nil)

func TestPresenceLastInputIsTheAttachedClientsActivity(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if at, err := h.LastInput(ctx); err != nil || !at.IsZero() {
		t.Fatalf("no server: LastInput = %v, %v; want zero and no error", at, err)
	}
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	if at, err := h.LastInput(ctx); err != nil || !at.IsZero() {
		t.Fatalf("no client: LastInput = %v, %v; want zero and no error", at, err)
	}
	attach := h.AttachCommand(slot)
	control := append(slices.Clone(attach[:len(attach)-3]), "-C", "attach-session", "-t", string(slot))
	cmd := exec.Command(control[0], control[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-2 * time.Second)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	var at time.Time
	waitFor(t, "the attached client's activity", func() bool {
		at, err = h.LastInput(ctx)
		return err == nil && !at.IsZero()
	})
	if at.Before(before) || at.After(time.Now().Add(time.Second)) {
		t.Fatalf("LastInput = %v, want about now", at)
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	waitFor(t, "no client after it detached", func() bool {
		at, err = h.LastInput(ctx)
		return err == nil && at.IsZero()
	})
}
