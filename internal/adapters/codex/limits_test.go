package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

func TestUsageSnapshotLimitsAreNamedLikeClaudesAndStampedWithTheReportTime(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "rollout", "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	snap, err := ReadSnapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.RateLimit{
		{Window: "five_hour", UsedPercent: 41, ResetsAt: 1791315501},
		{Window: "seven_day", UsedPercent: 12, ResetsAt: 1791800000},
	}
	if got := snap.RateLimits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if wantAt := time.Date(2026, 9, 29, 10, 5, 3, 0, time.UTC); !snap.LimitsAt.Equal(wantAt) {
		t.Fatalf("LimitsAt = %s, want %s", snap.LimitsAt, wantAt)
	}
}

func TestUsageSnapshotLimitsTimeIsTheNewestTokenCountThatCarriedLimits(t *testing.T) {
	rollout := `{"timestamp":"2026-09-29T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":10,"window_minutes":300,"resets_at":5}}}}` + "\n" +
		`{"timestamp":"2026-09-29T10:30:00Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":null}}` + "\n"
	snap, err := ReadSnapshot(strings.NewReader(rollout))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC); !snap.LimitsAt.Equal(want) {
		t.Fatalf("LimitsAt = %s, want %s", snap.LimitsAt, want)
	}
}

func TestUsageSnapshotWithoutLimitsHasNoRateLimits(t *testing.T) {
	if got := (Snapshot{}).RateLimits(); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
