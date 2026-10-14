package daemon_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestUsageStatusLineLimitsAreStampedWhenTheDaemonReceivesThem(t *testing.T) {
	c, sub := runningOn(t, "%3")
	before := time.Now()
	report := domain.StatusReport{Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 40}}}
	if err := c.Call(context.Background(), rpc.MethodStatusLine, rpc.StatusLine{Pane: "%3", Report: report}, nil); err != nil {
		t.Fatal(err)
	}
	diff := next(t, sub.Diffs)
	if diff.Session == nil || diff.Session.LimitsAt.Before(before) || time.Since(diff.Session.LimitsAt) > time.Minute {
		t.Fatalf("diff %+v", diff.Session)
	}
}

func TestUsageCodexRolloutLimitsLandOnTheSessionStampedWithTheRolloutTime(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessCodex, Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	payload, _ := json.Marshal(map[string]string{"transcript_path": codexRollout(t)})
	if err := c.Call(ctx, rpc.MethodHook, rpc.Hook{Harness: "codex", Event: "Stop", Pane: "%3", At: time.Now(), Payload: payload}, nil); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	got := next(t, sub.Diffs).Session
	want := []domain.RateLimit{
		{Window: "five_hour", UsedPercent: 41, ResetsAt: 1791315501},
		{Window: "seven_day", UsedPercent: 12, ResetsAt: 1791800000},
	}
	if got == nil || !reflect.DeepEqual(got.Limits, want) || !got.LimitsAt.Equal(time.Date(2026, 9, 29, 10, 5, 3, 0, time.UTC)) {
		t.Fatalf("session %+v", got)
	}
}

func TestModelSwitchCodexRolloutConfirmsOnlyFromATurnAfterTheSwitch(t *testing.T) {
	turn := time.Date(2026, 9, 29, 10, 5, 0, 0, time.UTC)
	cases := []struct {
		name        string
		sw          domain.Switch
		wantPending int
	}{
		{"a later turn shows it", domain.Switch{Kind: domain.SwitchEffort, Value: "medium", SentAt: turn.Add(-time.Minute)}, 0},
		{"an earlier turn says nothing yet", domain.Switch{Kind: domain.SwitchEffort, Value: "high", SentAt: turn.Add(time.Minute)}, 1},
	}
	for _, c := range cases {
		d, path := start(t, &memStore{})
		cl := dial(t, path)
		ctx := context.Background()
		sub, err := cl.Subscribe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessCodex, Pane: "%3", State: domain.StateRunning, Effort: "low", Switches: []domain.Switch{c.sw}}})
		next(t, sub.Diffs)
		payload, _ := json.Marshal(map[string]string{"transcript_path": codexRollout(t)})
		if err := cl.Call(ctx, rpc.MethodHook, rpc.Hook{Harness: "codex", Event: "Stop", Pane: "%3", At: time.Now(), Payload: payload}, nil); err != nil {
			t.Fatal(err)
		}
		next(t, sub.Diffs)
		got := next(t, sub.Diffs).Session
		if got == nil || got.Effort != "medium" || len(got.Switches) != c.wantPending || got.SwitchWarning {
			t.Fatalf("%s: session %+v", c.name, got)
		}
	}
}
