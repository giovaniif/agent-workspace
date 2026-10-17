package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

func TestSnapshotTakesTheLatestModelEffortContextAndLimits(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "rollout", "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	snap, err := ReadSnapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Model != "gpt-6.1-sol" || snap.Effort != "medium" {
		t.Fatalf("model %q effort %q", snap.Model, snap.Effort)
	}
	if snap.ContextLeftPercent != 56 {
		t.Fatalf("context left %d", snap.ContextLeftPercent)
	}
	want := []LimitWindow{
		{WindowMinutes: 300, UsedPercent: 40.6, ResetsAt: time.Unix(1791315501, 0)},
		{WindowMinutes: 10080, UsedPercent: 12, ResetsAt: time.Unix(1791800000, 0)},
	}
	if len(snap.Limits) != 2 || snap.Limits[0] != want[0] || snap.Limits[1] != want[1] {
		t.Fatalf("limits %+v", snap.Limits)
	}
	if got := snap.Usage(); got != (domain.Usage{ContextLeftPercent: 56, LimitUsedPercent: 41, Tokens: 140000}) {
		t.Fatalf("usage %+v", got)
	}
}

func TestSnapshotOfARolloutWithoutTokenCountsIsUnknown(t *testing.T) {
	snap, err := ReadSnapshot(strings.NewReader(`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","effort":"high"}}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Model != "gpt-6.1-sol" || snap.Effort != "high" || snap.ContextLeftPercent != UnknownPercent || len(snap.Limits) != 0 {
		t.Fatalf("got %+v", snap)
	}
}

func TestReadingOnlyTheTailOfALargeRolloutStillFindsTheLatest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	filler := `{"type":"response_item","payload":{"text":"` + strings.Repeat("x", 2000) + `"}}` + "\n"
	tail := `{"type":"turn_context","payload":{"model":"gpt-6.1-sol","effort":"low"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":12000},"model_context_window":112000}}}` + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(filler, 50)+tail), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := ReadSnapshotFile(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Model != "gpt-6.1-sol" || snap.Effort != "low" || snap.ContextLeftPercent != 100 {
		t.Fatalf("got %+v", snap)
	}
}

func TestContextLeftPercentIgnoresTheFixedPromptBaseline(t *testing.T) {
	cases := []struct {
		used, window, want int
	}{
		{12000, 258400, 100},
		{0, 258400, 100},
		{16539, 258400, 98},
		{135200, 258400, 50},
		{258400, 258400, 0},
		{300000, 258400, 0},
		{5000, 12000, 0},
		{5000, 0, 0},
	}
	for _, c := range cases {
		if got := ContextLeftPercent(c.used, c.window); got != c.want {
			t.Errorf("ContextLeftPercent(%d, %d) = %d, want %d", c.used, c.window, got, c.want)
		}
	}
}

func TestSnapshotTurnAtIsTheLatestTurnContextTime(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "rollout", "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	snap, err := ReadSnapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 29, 10, 5, 0, 0, time.UTC); !snap.TurnAt.Equal(want) {
		t.Fatalf("TurnAt %v", snap.TurnAt)
	}
}
