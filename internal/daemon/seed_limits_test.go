package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func seededQuotas(t *testing.T, p rpc.DebugSeedParams) (map[domain.Harness]int, bool) {
	t.Helper()
	_, path := start(t, &memStore{})
	ctx := context.Background()
	if err := dial(t, path).DebugSeed(ctx, p); err != nil {
		t.Fatal(err)
	}
	sub, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sub.State.Sessions {
		if !p.Codex && s.Harness == domain.HarnessCodex {
			t.Errorf("seeded a Codex session without asking: %+v", s)
		}
	}
	seen := map[domain.Harness]int{}
	low := false
	for _, q := range domain.Quotas(sub.State.Sessions) {
		seen[q.Harness]++
		low = low || q.Low()
		if q.Stale(time.Now()) {
			t.Errorf("seeded quota is stale: %+v", q)
		}
	}
	return seen, low
}

func TestUsageDebugSeedIsClaudeOnlyWithALowLimit(t *testing.T) {
	seen, low := seededQuotas(t, rpc.DebugSeedParams{Count: 3})
	if seen[domain.HarnessClaude] < 2 || seen[domain.HarnessCodex] != 0 || !low {
		t.Fatalf("quotas by harness %v, low %v", seen, low)
	}
}

func TestUsageDebugSeedWithCodexGivesEachHarnessFreshLimits(t *testing.T) {
	seen, low := seededQuotas(t, rpc.DebugSeedParams{Count: 2, Codex: true})
	if seen[domain.HarnessClaude] < 2 || seen[domain.HarnessCodex] < 2 || !low {
		t.Fatalf("quotas by harness %v, low %v", seen, low)
	}
}
