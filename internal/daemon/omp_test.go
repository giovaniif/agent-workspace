package daemon_test

import (
	"context"
	"slices"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/adapters/omp"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func nextSession(t *testing.T, diffs <-chan rpc.Diff) domain.Session {
	t.Helper()
	for {
		if d := next(t, diffs); d.Session != nil {
			return *d.Session
		}
	}
}

func TestOmpSessionLaunchesAndMovesOnItsOwnHookNames(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}, codex.Adapter{}, omp.Adapter{}))
	c := dial(t, path)
	ctx := context.Background()
	var s domain.Session
	if err := c.Call(ctx, rpc.MethodLaunch, rpc.LaunchParams{Harness: "omp", Dir: "/w", Model: "sonnet"}, &s); err != nil {
		t.Fatal(err)
	}
	if s.Harness != domain.HarnessOmp || len(host.specs) != 1 || !slices.Equal(host.specs[0].Command, []string{"omp", "--model", "sonnet"}) {
		t.Fatalf("session %+v, specs %+v", s, host.specs)
	}
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fire := func(event, payload string) {
		t.Helper()
		h := rpc.Hook{Harness: "omp", Event: event, Pane: s.Pane, Payload: []byte(payload)}
		if err := c.Call(ctx, rpc.MethodHook, h, nil); err != nil {
			t.Fatal(err)
		}
	}

	fire("agent_start", `{"model":"opus","effort":"high"}`)
	got := nextSession(t, sub.Diffs)
	if got.State != domain.StateRunning || got.Model != "opus" || got.Effort != "high" {
		t.Fatalf("after agent_start: state %s model %q effort %q", got.State, got.Model, got.Effort)
	}
	fire("turn_end", `{}`)
	fire("agent_end", `{}`)
	fire("tool_approval_requested", `{"tool_name":"bash"}`)
	if got := nextSession(t, sub.Diffs); got.State != domain.StatePermission {
		t.Fatalf("turn_end or agent_end moved the session: state %s", got.State)
	}
	fire("session_stop", `{"last_assistant_message":"Added it."}`)
	if got := nextSession(t, sub.Diffs); got.State != domain.StateDone {
		t.Fatalf("after session_stop: state %s", got.State)
	}
}

func TestOnboardingInstallsOmp(t *testing.T) {
	f := &fakeOnboarder{}
	_, path := start(t, &memStore{}, daemon.WithOnboarding(f))
	setup, err := dial(t, path).OnboardInstall(context.Background(), domain.HarnessOmp)
	if err != nil || setup != (domain.HarnessSetup{Installed: true, File: "/c/omp"}) {
		t.Fatalf("install = %+v, %v", setup, err)
	}
}
