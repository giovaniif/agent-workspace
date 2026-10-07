//go:build integration

package tmux_test

import (
	"context"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
)

const tabSwitchBudget = 60 * time.Millisecond

func TestTabSwitchSwapsEachTabIntoTheSlotAndKeepsTheOthersAlive(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	var tabs []app.PaneID
	for _, name := range []string{"agent", "shell", "agent-2"} {
		pane, err := h.Create(ctx, app.PaneSpec{Name: name, Command: []string{"sleep", "600"}})
		if err != nil {
			t.Fatal(err)
		}
		tabs = append(tabs, pane)
	}
	var slowest time.Duration
	for round := range 3 {
		for _, pane := range tabs {
			began := time.Now()
			if err := h.Show(ctx, pane, slot); err != nil {
				t.Fatal(err)
			}
			slowest = max(slowest, time.Since(began))
			if got := h.ShownIn(ctx, slot); got != pane {
				t.Fatalf("round %d: slot shows %s, want %s", round, got, pane)
			}
		}
	}
	for _, pane := range tabs {
		if alive, err := h.Alive(ctx, pane); err != nil || !alive {
			t.Fatalf("tab %s died while switching: %v", pane, err)
		}
	}
	if slowest > tabSwitchBudget {
		t.Errorf("slowest tab switch took %v; the budget is %v", slowest, tabSwitchBudget)
	}
}
