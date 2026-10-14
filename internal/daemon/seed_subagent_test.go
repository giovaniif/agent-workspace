package daemon_test

import (
	"context"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestSubagentDebugSeedGivesASessionThreeSubagentsWithARunningOne(t *testing.T) {
	_, path := start(t, &memStore{})
	ctx := context.Background()
	if err := dial(t, path).DebugSeed(ctx, rpc.DebugSeedParams{Count: 3}); err != nil {
		t.Fatal(err)
	}
	sub, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bySession := map[string][]domain.Subagent{}
	for _, s := range sub.State.Subagents {
		bySession[s.SessionID] = append(bySession[s.SessionID], s)
	}
	if len(bySession) != 1 {
		t.Fatalf("subagents on %d sessions", len(bySession))
	}
	for _, subs := range bySession {
		running := 0
		for _, s := range subs {
			if s.State == domain.SubagentRunning {
				running++
			}
		}
		if len(subs) != 3 || running == 0 {
			t.Fatalf("subagents %+v", subs)
		}
	}
}
