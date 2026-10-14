package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func runningOn(t *testing.T, pane string, opts ...daemon.Option) (*rpc.Client, rpc.Subscription) {
	t.Helper()
	d, path := start(t, &memStore{}, opts...)
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: pane, State: domain.StateRunning}})
	next(t, sub.Diffs)
	return c, sub
}

func TestClaudeNotificationTypeDecidesTheState(t *testing.T) {
	cases := map[string]domain.AgentState{
		`{"notification_type":"permission_prompt"}`: domain.StatePermission,
		`{"notification_type":"idle_prompt"}`:       domain.StateWaiting,
	}
	for payload, want := range cases {
		c, sub := runningOn(t, "%3")
		h := rpc.Hook{Harness: "claude", Event: "Notification", Pane: "%3", Payload: json.RawMessage(payload)}
		if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
			t.Fatal(err)
		}
		if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.State != want {
			t.Fatalf("%s: diff %+v", payload, diff)
		}
	}
}

func TestStatusLineReportUpdatesTheSessionOnItsPane(t *testing.T) {
	c, sub := runningOn(t, "%3")
	report := domain.StatusReport{Model: "Opus 5.5", Effort: "high", ContextLeft: 80, HasContext: true,
		Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 40}, {Window: "seven_day", UsedPercent: 65}}}
	if err := c.Call(context.Background(), rpc.MethodStatusLine, rpc.StatusLine{Pane: "%3", Report: report}, nil); err != nil {
		t.Fatal(err)
	}
	diff := next(t, sub.Diffs)
	if diff.Session == nil {
		t.Fatalf("diff %+v", diff)
	}
	got := *diff.Session
	if got.Model != "Opus 5.5" || got.Effort != "high" || got.State != domain.StateRunning ||
		got.Usage.ContextLeftPercent != 80 || got.Usage.LimitUsedPercent != 65 || !reflect.DeepEqual(got.Limits, report.Limits) {
		t.Fatalf("session %+v", got)
	}
}

func TestStatusLineFromAnUnknownPaneIsIgnored(t *testing.T) {
	c, sub := runningOn(t, "%3")
	if err := c.Call(context.Background(), rpc.MethodStatusLine, rpc.StatusLine{Pane: "%9", Report: domain.StatusReport{Model: "x"}}, nil); err != nil {
		t.Fatal(err)
	}
	h := rpc.Hook{Harness: "claude", Event: "Stop", Pane: "%3"}
	if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.Model != "" || diff.Session.State != domain.StateDone {
		t.Fatalf("diff %+v", diff)
	}
}

func TestLaunchOpensAPaneAndAddsAnIdleSessionOnIt(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	var got domain.Session
	params := rpc.LaunchParams{Harness: "claude", Name: "api", Dir: "/w/api", Model: "sonnet", Effort: "high"}
	if err := c.Call(context.Background(), rpc.MethodLaunch, params, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.Pane != "%7" || got.Harness != domain.HarnessClaude || got.State != domain.StateIdle || got.Model != "sonnet" || got.Effort != "high" {
		t.Fatalf("session %+v", got)
	}
	want := []app.PaneSpec{{Name: "api", Dir: "/w/api", Command: []string{"claude", "--model", "sonnet", "--effort", "high"}}}
	if !reflect.DeepEqual(host.specs, want) {
		t.Fatalf("specs %+v", host.specs)
	}
	st, err := c.Status(context.Background())
	if err != nil || st.Sessions != 1 {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestLaunchErrors(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	var rerr *rpc.Error
	err := c.Call(context.Background(), rpc.MethodLaunch, rpc.LaunchParams{Harness: "other", Dir: "/w"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("unknown harness: %v", err)
	}
	host.err = errors.New("tmux down")
	err = c.Call(context.Background(), rpc.MethodLaunch, rpc.LaunchParams{Harness: "claude", Dir: "/w"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeLaunchFailed {
		t.Fatalf("host failure: %v", err)
	}
	st, _ := c.Status(context.Background())
	if st.Sessions != 0 {
		t.Fatalf("sessions %d", st.Sessions)
	}
}
