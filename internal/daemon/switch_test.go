package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func switchSetup(t *testing.T, state domain.AgentState) (*rpc.Client, rpc.Subscription, *fakeHost) {
	t.Helper()
	host := &fakeHost{}
	d, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", Model: "Sonnet 4.6", State: state}})
	next(t, sub.Diffs)
	return c, sub, host
}

func TestModelSwitchIsTypedIntoTheIdleSessionsPane(t *testing.T) {
	c, sub, host := switchSetup(t, domain.StateIdle)
	got, err := c.SwitchSession(context.Background(), "a", domain.SwitchModel, "opus")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Switches) != 1 || got.Switches[0].SentAt.IsZero() {
		t.Fatalf("session %+v", got.Switches)
	}
	if diff := next(t, sub.Diffs); diff.Session == nil || len(diff.Session.Switches) != 1 {
		t.Fatalf("diff %+v", diff)
	}
	want := []string{"%3 paste=true /model opus", "%3 keys Enter"}
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestModelSwitchWhileRunningWaitsForTheNextStop(t *testing.T) {
	c, sub, host := switchSetup(t, domain.StateRunning)
	got, err := c.SwitchSession(context.Background(), "a", domain.SwitchEffort, "high")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Switches) != 1 || !got.Switches[0].SentAt.IsZero() {
		t.Fatalf("session %+v", got.Switches)
	}
	next(t, sub.Diffs)
	if typed := host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed while running: %q", typed)
	}
	stop := rpc.Hook{Harness: "claude", Event: "Stop", Pane: "%3"}
	if err := c.Call(context.Background(), rpc.MethodHook, stop, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.State != domain.StateDone || diff.Session.Switches[0].SentAt.IsZero() {
		t.Fatalf("diff %+v", diff)
	}
	want := []string{"%3 paste=true /effort high", "%3 keys Enter"}
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestModelSwitchIsConfirmedByTheNextStatusLine(t *testing.T) {
	c, sub, _ := switchSetup(t, domain.StateIdle)
	if _, err := c.SwitchSession(context.Background(), "a", domain.SwitchModel, "opus"); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	report := rpc.StatusLine{Pane: "%3", Report: domain.StatusReport{Model: "Opus 4.7"}}
	if err := c.Call(context.Background(), rpc.MethodStatusLine, report, nil); err != nil {
		t.Fatal(err)
	}
	diff := next(t, sub.Diffs)
	if diff.Session == nil || diff.Session.Model != "Opus 4.7" || len(diff.Session.Switches) != 0 || diff.Session.SwitchWarning {
		t.Fatalf("diff %+v", diff.Session)
	}
}

func TestModelSwitchWarnsWhenTheNextStatusLineStillShowsTheOldModel(t *testing.T) {
	c, sub, _ := switchSetup(t, domain.StateIdle)
	if _, err := c.SwitchSession(context.Background(), "a", domain.SwitchModel, "opus"); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	report := rpc.StatusLine{Pane: "%3", Report: domain.StatusReport{Model: "Sonnet 4.6"}}
	if err := c.Call(context.Background(), rpc.MethodStatusLine, report, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, sub.Diffs); diff.Session == nil || !diff.Session.SwitchWarning {
		t.Fatalf("diff %+v", diff.Session)
	}
}

func TestModelSwitchThePaneRefusesRaisesTheWarning(t *testing.T) {
	c, sub, host := switchSetup(t, domain.StateIdle)
	host.failOn("/model opus")
	if _, err := c.SwitchSession(context.Background(), "a", domain.SwitchModel, "opus"); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	diff := next(t, sub.Diffs)
	if diff.Session == nil || !diff.Session.SwitchWarning || len(diff.Session.Switches) != 0 {
		t.Fatalf("diff %+v", diff.Session)
	}
}

func TestModelSwitchRequestErrors(t *testing.T) {
	c, _, _ := switchSetup(t, domain.StateIdle)
	cases := []struct {
		id, kind, value, code string
	}{
		{"nope", "model", "opus", rpc.CodeNotFound},
		{"a", "flavor", "opus", rpc.CodeBadRequest},
		{"a", "model", "", rpc.CodeBadRequest},
	}
	for _, tc := range cases {
		_, err := c.SwitchSession(context.Background(), tc.id, domain.SwitchKind(tc.kind), tc.value)
		var rerr *rpc.Error
		if !errors.As(err, &rerr) || rerr.Code != tc.code {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestModelSwitchSupersededWhileWaitingForTheSendLockIsNeverTyped(t *testing.T) {
	host := &fakeHost{}
	d, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []domain.Session{
		{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateIdle},
		{ID: "b", Harness: domain.HarnessClaude, Pane: "%4", State: domain.StateIdle},
	} {
		d.Post(daemon.SessionChanged{Session: s})
		next(t, sub.Diffs)
	}
	release := host.holdPane("%3")
	for _, req := range [][2]string{{"a", "opus"}, {"b", "sonnet"}, {"b", "haiku"}} {
		if _, err := c.SwitchSession(context.Background(), req[0], domain.SwitchModel, req[1]); err != nil {
			t.Fatal(err)
		}
		next(t, sub.Diffs)
		if req[0] == "a" {
			host.waitHeld(t, "%3")
		}
	}
	release()

	want := []string{"%3 paste=true /model opus", "%3 keys Enter", "%4 paste=true /model haiku", "%4 keys Enter"}
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestModelSwitchCodexOpensItsModelPickerAndPicksTheRow(t *testing.T) {
	host := &fakeHost{screens: []string{"  Select Model and Effort\n› 1. GPT-6.1-Sol (current)  x\n  2. GPT-6-Luna  y"}}
	d, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "x", Harness: domain.HarnessCodex, Pane: "%5", State: domain.StateIdle}})
	next(t, sub.Diffs)
	if _, err := c.SwitchSession(context.Background(), "x", domain.SwitchModel, "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	want := []string{"%5 paste=true /model", "%5 keys Enter", "%5 keys Down Enter"}
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestModelSwitchWaitingBehindAnotherPaneIsHeldIfTheSessionStartsRunning(t *testing.T) {
	host := &fakeHost{}
	d, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []domain.Session{
		{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateIdle},
		{ID: "b", Harness: domain.HarnessClaude, Pane: "%4", State: domain.StateIdle},
	} {
		d.Post(daemon.SessionChanged{Session: s})
		next(t, sub.Diffs)
	}
	release := host.holdPane("%3")
	if _, err := c.SwitchSession(context.Background(), "a", domain.SwitchModel, "opus"); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	host.waitHeld(t, "%3")
	if _, err := c.SwitchSession(context.Background(), "b", domain.SwitchModel, "sonnet"); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	prompt := rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: "%4"}
	if err := c.Call(context.Background(), rpc.MethodHook, prompt, nil); err != nil {
		t.Fatal(err)
	}
	next(t, sub.Diffs)
	release()

	requeued := next(t, sub.Diffs)
	if requeued.Session == nil || requeued.Session.ID != "b" || len(requeued.Session.Switches) != 1 || !requeued.Session.Switches[0].SentAt.IsZero() {
		t.Fatalf("diff %+v", requeued.Session)
	}
	want := []string{"%3 paste=true /model opus", "%3 keys Enter"}
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
	stop := rpc.Hook{Harness: "claude", Event: "Stop", Pane: "%4"}
	if err := c.Call(context.Background(), rpc.MethodHook, stop, nil); err != nil {
		t.Fatal(err)
	}
	want = append(want, "%4 paste=true /model sonnet", "%4 keys Enter")
	if typed := host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}
