package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func hookOnPane(t *testing.T, c *rpc.Client, event, payload string) {
	t.Helper()
	h := rpc.Hook{Harness: "claude", Event: event, Pane: "%3", At: time.Now(), Payload: json.RawMessage(payload)}
	if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestHookRecordsASessionEventAndPublishesItWithTheSessionChange(t *testing.T) {
	store := &memStore{}
	d, path := start(t, store)
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	hookOnPane(t, c, "PreToolUse", `{"tool_name":"Bash","tool_input":{"command":"make"}}`)
	diff := next(t, sub.Diffs)
	if diff.Session == nil || diff.Session.ID != "a" || diff.Event == nil || diff.Event.SessionID != "a" || diff.Event.Kind != domain.EventPreToolUse || diff.Event.Tool != "Bash" || diff.Event.Detail != "make" {
		t.Fatalf("diff %+v", diff)
	}
	snap, _ := store.Load()
	if len(snap.Events) != 1 || snap.Events[0].SessionID != "a" {
		t.Fatalf("stored events %+v", snap.Events)
	}
	again, err := dial(t, path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.State.Events) != 1 || again.State.Events[0].Tool != "Bash" {
		t.Fatalf("snapshot events %+v", again.State.Events)
	}
}

func TestSnapshotKeepsOnlyTheNewestEventsOfASession(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}})
	waitStatus(t, c, func(s rpc.Status) bool { return s.Sessions == 1 })
	for i := range app.EventsPerSession + 3 {
		hookOnPane(t, c, "PreToolUse", fmt.Sprintf(`{"tool_name":"Bash","tool_input":{"command":"step %d"}}`, i))
	}
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events := sub.State.Events
	if len(events) != app.EventsPerSession {
		t.Fatalf("kept %d events", len(events))
	}
	if events[0].Detail != "step 3" || events[len(events)-1].Detail != fmt.Sprintf("step %d", app.EventsPerSession+2) {
		t.Fatalf("kept %q to %q", events[0].Detail, events[len(events)-1].Detail)
	}
}

func TestRestoredEventsAreInTheSnapshot(t *testing.T) {
	restored := []domain.SessionEvent{{SessionID: "a", Kind: domain.EventStop, Text: "Which branch?"}}
	_, path := start(t, &memStore{snap: app.Snapshot{Events: restored}})
	sub, err := dial(t, path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.State.Events) != 1 || sub.State.Events[0].Text != "Which branch?" {
		t.Fatalf("events %+v", sub.State.Events)
	}
}

func TestDebugSeedGivesEverySessionAnEventLogForTheCard(t *testing.T) {
	_, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	if err := c.DebugSeed(ctx, rpc.DebugSeedParams{Count: 5}); err != nil {
		t.Fatal(err)
	}
	sub, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sub.State.Sessions {
		card := domain.BuildSessionCard(domain.Task{}, s, nil, sub.State.Events)
		if len(card.Actions) == 0 {
			t.Errorf("%s (%s) has no actions", s.ID, s.State)
		}
		needsReason := s.State == domain.StatePermission || s.State == domain.StateWaiting
		if needsReason && card.Waiting == "" {
			t.Errorf("%s (%s) waits on nothing", s.ID, s.State)
		}
		if !needsReason && card.Waiting != "" {
			t.Errorf("%s (%s) waits on %q", s.ID, s.State, card.Waiting)
		}
	}
}
