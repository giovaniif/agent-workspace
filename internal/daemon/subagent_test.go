package daemon_test

import (
	"context"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
)

func subagentDiff(t *testing.T, sub rpc.Subscription) rpc.Diff {
	t.Helper()
	for range 3 {
		if d := next(t, sub.Diffs); d.Subagent != nil {
			return d
		}
	}
	t.Fatal("no subagent diff")
	return rpc.Diff{}
}

func TestSubagentHooksBuildTheSessionsSubagentDiffs(t *testing.T) {
	c, sub := runningOn(t, "%3")
	hookOnPane(t, c, "SubagentStart", `{"agent_id":"a1","agent_type":"Explore"}`)
	started := subagentDiff(t, sub).Subagent
	if started.SessionID != "a" || started.ID != "a1" || started.Type != "Explore" || started.State != "running" {
		t.Fatalf("start %+v", started)
	}
	hookOnPane(t, c, "SubagentStop", `{"agent_id":"a1","agent_type":"Explore","last_assistant_message":"Found it."}`)
	stopped := subagentDiff(t, sub).Subagent
	if stopped.State != "stopped" || stopped.Summary != "Found it." || !stopped.StartedAt.Equal(started.StartedAt) {
		t.Fatalf("stop %+v", stopped)
	}
}

func TestSubagentHookKeepsTheParentTurnRunning(t *testing.T) {
	c, sub := runningOn(t, "%3")
	hookOnPane(t, c, "SubagentStop", `{"agent_id":"a1"}`)
	d := next(t, sub.Diffs)
	if d.Session == nil || d.Session.State != "running" || d.Subagent != nil {
		t.Fatalf("diff %+v", d)
	}
}

func TestSubagentsAreInTheSnapshotAndEndWithTheirSession(t *testing.T) {
	c, sub := runningOn(t, "%3")
	hookOnPane(t, c, "SubagentStart", `{"agent_id":"a1","agent_type":"Explore"}`)
	hookOnPane(t, c, "SubagentStart", `{"agent_id":"a2","agent_type":"Plan"}`)
	hookOnPane(t, c, "SubagentStop", `{"agent_id":"a2"}`)
	hookOnPane(t, c, "SessionEnd", `{}`)
	for range 3 {
		subagentDiff(t, sub)
	}
	ended := subagentDiff(t, sub).Subagent
	if ended.ID != "a1" || ended.State != "stopped" {
		t.Fatalf("session end changed %+v", ended)
	}
	again, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := again.State.Subagents
	if len(got) != 2 || got[0].ID != "a1" || got[0].State != "stopped" || got[1].ID != "a2" || got[1].State != "stopped" {
		t.Fatalf("snapshot %+v", got)
	}
}
