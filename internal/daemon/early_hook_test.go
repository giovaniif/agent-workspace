package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/rpc"
)

func TestEarlyHooksKeepAtMost64PanesAndTheNewestPane(t *testing.T) {
	s := &state{earlyHooks: map[string][]earlyHook{}}
	now := time.Unix(1000, 0)
	for i := range 1000 {
		s.holdEarlyHook(rpc.Hook{Pane: fmt.Sprintf("%%%d", i)}, now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(s.earlyHooks) > 64 {
		t.Fatalf("held hooks for %d panes, want at most 64", len(s.earlyHooks))
	}
	if len(s.earlyHooks["%999"]) != 1 {
		t.Fatalf("newest pane hooks = %v, want 1", s.earlyHooks["%999"])
	}
}

func TestEarlyHooksDropAnExpiredPaneBeforeTheOldestLivePane(t *testing.T) {
	s := &state{earlyHooks: map[string][]earlyHook{}}
	now := time.Unix(1000, 0)
	s.holdEarlyHook(rpc.Hook{Pane: "%old"}, now)
	for i := range 63 {
		s.holdEarlyHook(rpc.Hook{Pane: fmt.Sprintf("%%%d", i)}, now.Add(31*time.Second))
	}
	s.holdEarlyHook(rpc.Hook{Pane: "%new"}, now.Add(31*time.Second))
	if _, ok := s.earlyHooks["%old"]; ok {
		t.Fatal("expired pane is still held")
	}
	if len(s.earlyHooks["%0"]) != 1 || len(s.earlyHooks["%new"]) != 1 {
		t.Fatalf("live panes were dropped: %d panes held", len(s.earlyHooks))
	}
}
