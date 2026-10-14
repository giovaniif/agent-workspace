package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func sessionDiff(seq uint64, s domain.Session) rpc.Diff {
	return rpc.Diff{Seq: seq, Session: &s}
}

func TestDebugSessionPrintsTheCurrentStateThenEachChangeToThatSession(t *testing.T) {
	running := domain.Session{ID: "s1", Harness: domain.HarnessCodex, Pane: "%3", State: domain.StateRunning, Model: "gpt-6.1-sol", Effort: "high"}
	done := running
	done.State = domain.StateDone
	done.Usage = domain.Usage{ContextLeftPercent: 56, LimitUsedPercent: 41}

	diffs := make(chan rpc.Diff, 4)
	diffs <- sessionDiff(8, domain.Session{ID: "other", State: domain.StatePermission})
	diffs <- rpc.Diff{Seq: 9, Worktree: &domain.Worktree{ID: "w"}}
	diffs <- sessionDiff(10, done)
	close(diffs)
	sub := rpc.Subscription{State: rpc.State{Seq: 7, Sessions: []domain.Session{running, {ID: "other"}}}, Diffs: diffs}

	var out bytes.Buffer
	if err := streamSession(context.Background(), sub, "s1", true, &out); err != nil {
		t.Fatal(err)
	}
	want := "seq=7 state=running harness=codex pane=%3 model=gpt-6.1-sol effort=high context_left=0% limit_used=0%\n" +
		"seq=10 state=done harness=codex pane=%3 model=gpt-6.1-sol effort=high context_left=56% limit_used=41%\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestDebugSessionWithoutFollowStopsAfterTheCurrentState(t *testing.T) {
	diffs := make(chan rpc.Diff)
	sub := rpc.Subscription{State: rpc.State{Sessions: []domain.Session{{ID: "s1", State: domain.StateIdle}}}, Diffs: diffs}
	var out bytes.Buffer
	if err := streamSession(context.Background(), sub, "s1", false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "seq=0 state=idle") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("got %q", out.String())
	}
}

func TestDebugSessionFailsForAnUnknownSession(t *testing.T) {
	sub := rpc.Subscription{State: rpc.State{Sessions: []domain.Session{{ID: "s1"}}}, Diffs: make(chan rpc.Diff)}
	err := streamSession(context.Background(), sub, "nope", true, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err %v", err)
	}
}

func TestDebugSessionFollowEndsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := rpc.Subscription{State: rpc.State{Sessions: []domain.Session{{ID: "s1"}}}, Diffs: make(chan rpc.Diff)}
	if err := streamSession(ctx, sub, "s1", true, &bytes.Buffer{}); err != nil {
		t.Fatalf("err %v", err)
	}
}
