package daemon_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func mateOn(t *testing.T, seat *domain.MateSeat) (*daemon.Daemon, *rpc.Client, rpc.Subscription) {
	t.Helper()
	d, path := start(t, &memStore{})
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "m", Harness: domain.HarnessClaude, Pane: "%5", Dir: "/w/api", State: domain.StateRunning, Mate: seat}})
	next(t, sub.Diffs)
	return d, c, sub
}

func preToolUse(t *testing.T, c *rpc.Client, cmd string, withAction bool) rpc.HookReply {
	t.Helper()
	input, _ := json.Marshal(map[string]string{"command": cmd})
	payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": json.RawMessage(input), "cwd": "/w/api"})
	h := rpc.Hook{Harness: "claude", Event: "PreToolUse", Pane: "%5", Payload: payload}
	if withAction {
		a := domain.ClassifyAction(domain.HarnessClaude, "Bash", input, domain.ActionScope{Root: "/w/api"})
		h.Action = &a
	}
	var reply rpc.HookReply
	if err := c.Call(context.Background(), rpc.MethodHook, h, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func pendingApprovals(t *testing.T, c *rpc.Client) []domain.Approval {
	t.Helper()
	var got []domain.Approval
	if err := c.Call(context.Background(), rpc.MethodApprovalList, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNonOwnerTurnOfAMateIsDeniedAGitPushAndAnApprovalWaits(t *testing.T) {
	_, c, _ := mateOn(t, &domain.MateSeat{Name: "ada", Owner: "UOWNER", Authority: "UOTHER"})
	reply := preToolUse(t, c, "go test ./... && git push", true)
	if !strings.Contains(string(reply.Output), `"permissionDecision":"deny"`) || !strings.Contains(string(reply.Output), "waiting for owner approval") {
		t.Fatalf("reply %s", reply.Output)
	}
	got := pendingApprovals(t, c)
	if len(got) != 1 || got[0].SessionID != "m" || got[0].State != domain.ApprovalPending || got[0].Action.Kind != domain.ActionPush || got[0].ID == "" {
		t.Fatalf("approvals %+v", got)
	}
	preToolUse(t, c, "go test ./... && git push", true)
	if got := pendingApprovals(t, c); len(got) != 1 {
		t.Fatalf("the same action twice made %d approvals", len(got))
	}
}

func TestOwnerTurnOfAMateRunsAGitPush(t *testing.T) {
	d, c, sub := mateOn(t, &domain.MateSeat{Name: "ada", Owner: "UOWNER", Authority: "UOTHER"})
	d.Post(daemon.MateTurn{SessionID: "m", User: "UOWNER"})
	if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.Mate == nil || diff.Session.Mate.Authority != "UOWNER" {
		t.Fatalf("diff %+v", diff)
	}
	if reply := preToolUse(t, c, "git push", true); len(reply.Output) != 0 {
		t.Fatalf("reply %s", reply.Output)
	}
	if got := pendingApprovals(t, c); len(got) != 0 {
		t.Fatalf("approvals %+v", got)
	}
}

func TestMateGateClassifiesWhenTheHookSentNoAction(t *testing.T) {
	_, c, _ := mateOn(t, &domain.MateSeat{Name: "ada", Owner: "UOWNER", Authority: "UOTHER"})
	if reply := preToolUse(t, c, "git push", false); !strings.Contains(string(reply.Output), "deny") {
		t.Fatalf("reply %s", reply.Output)
	}
	if reply := preToolUse(t, c, "go test ./...", false); len(reply.Output) != 0 {
		t.Fatalf("safe reply %s", reply.Output)
	}
}

func TestMateTurnOnASessionThatIsNotAMateChangesNothing(t *testing.T) {
	d, c, sub := mateOn(t, nil)
	d.Post(daemon.MateTurn{SessionID: "m", User: "UOWNER"})
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "z", Pane: "%9"}})
	if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.ID != "z" {
		t.Fatalf("diff %+v", diff)
	}
	if reply := preToolUse(t, c, "git push", true); len(reply.Output) != 0 {
		t.Fatalf("a session that is not a mate got %s", reply.Output)
	}
	if got := pendingApprovals(t, c); len(got) != 0 {
		t.Fatalf("approvals %+v", got)
	}
}
