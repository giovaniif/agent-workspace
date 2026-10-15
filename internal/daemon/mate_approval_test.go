package daemon_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type approvalClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *approvalClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *approvalClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type approvalRig struct {
	mateRig
	m     *fakeSlackMate
	clock *approvalClock
}

func startApprovalRig(t *testing.T, store *memStore, extra ...daemon.Option) approvalRig {
	t.Helper()
	store.snap.Sessions = append(store.snap.Sessions, domain.Session{ID: "s1", Harness: domain.HarnessClaude, Pane: "%7", Dir: "/w/api", State: domain.StateIdle,
		Mate: &domain.MateSeat{Name: "helper", Owner: mateOwner, Authority: mateGuest}})
	store.snap.MateThreads = append(store.snap.MateThreads, domain.MateThread{Team: "T0TEAM001", Channel: "C0CHAN001", ThreadTS: "1700.0001", Mate: "helper", SessionID: "s1", Status: "1800.0001"})
	clock := &approvalClock{now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	r := startMateRig(t, store, append([]daemon.Option{daemon.WithClock(clock.Now)}, extra...)...)
	return approvalRig{mateRig: r, m: r.connect(t, "helper"), clock: clock}
}

func (r approvalRig) hook(t *testing.T, cmd string) rpc.HookReply {
	t.Helper()
	input, _ := json.Marshal(map[string]string{"command": cmd})
	payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": json.RawMessage(input), "cwd": "/w/api"})
	a := domain.ClassifyAction(domain.HarnessClaude, "Bash", input, domain.ActionScope{Root: "/w/api"})
	h := rpc.Hook{Harness: "claude", Event: "PreToolUse", Pane: "%7", At: time.Now(), Payload: payload, Action: &a}
	var reply rpc.HookReply
	if err := r.c.Call(context.Background(), rpc.MethodHook, h, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func denied(reply rpc.HookReply) bool {
	return strings.Contains(string(reply.Output), `"permissionDecision":"deny"`)
}

func (r approvalRig) asks() []fakeSlackAsk {
	m := r.hub.snapshot()
	return m[len(m)-1].asks
}

func (r approvalRig) approvalUpdates() []fakeSlackPost {
	var out []fakeSlackPost
	for _, u := range r.updates() {
		if strings.HasPrefix(u.thread, "1900.") {
			out = append(out, u)
		}
	}
	return out
}

func (r approvalRig) click(user, actionID, id string) {
	r.hub.send(r.m, app.SlackEvent{Kind: app.SlackAction, Team: "T0TEAM001", Channel: "C0CHAN001", User: user, TS: "1900.0001", ThreadTS: "1700.0001", ActionID: actionID, Value: id})
}

func (r approvalRig) pendingOne(t *testing.T) domain.Approval {
	t.Helper()
	r.hook(t, "git push")
	var got []domain.Approval
	waitFor(t, func() bool { got = pendingApprovals(t, r.c); return len(got) == 1 })
	return got[0]
}

func TestMateApprovalAsksTheOwnerInTheThreadAndStoresTheRequest(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	if reply := r.hook(t, "git push"); !denied(reply) {
		t.Fatalf("reply %s", reply.Output)
	}
	waitFor(t, func() bool { return len(r.asks()) == 1 })
	ask := r.asks()[0]
	got := pendingApprovals(t, r.c)
	if len(got) != 1 || ask.id != got[0].ID || ask.channel != "C0CHAN001" || ask.thread != "1700.0001" || !strings.Contains(ask.text, "<@"+mateGuest+">") || !strings.Contains(ask.text, "git push") {
		t.Fatalf("ask %+v approvals %+v", ask, got)
	}
	a := got[0]
	if a.Mate != "helper" || a.Requester != mateGuest || a.Channel != "C0CHAN001" || a.ThreadTS != "1700.0001" {
		t.Fatalf("approval %+v", a)
	}
	waitFor(t, func() bool {
		s := r.store.approvals()
		return len(s) == 1 && s[0].ID == a.ID && s[0].State == domain.ApprovalPending && s[0].Message == "1900.0001"
	})
}

func TestMateApprovalClickOfANonOwnerDoesNothing(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	waitFor(t, func() bool { return len(r.asks()) == 1 })
	r.click(mateGuest, domain.ApprovalApproveAction, a.ID)
	time.Sleep(50 * time.Millisecond)
	if got := pendingApprovals(t, r.c); len(got) != 1 || got[0].State != domain.ApprovalPending {
		t.Fatalf("approvals %+v", got)
	}
	if !denied(r.hook(t, "git push")) {
		t.Fatal("a click of a guest let the push run")
	}
	if ups := r.approvalUpdates(); len(ups) != 0 {
		t.Fatalf("updates %+v", ups)
	}
	if p := r.posts(); len(p) != 0 {
		t.Fatalf("posts %+v", p)
	}
	if r.typed("approved") {
		t.Fatal("the session got a retry")
	}
}

func TestMateApprovalOfTheOwnerAllowsTheSameInputOneTime(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	waitFor(t, func() bool { return len(r.asks()) == 1 })
	r.click(mateOwner, domain.ApprovalApproveAction, a.ID)
	waitFor(t, func() bool { return len(pendingApprovals(t, r.c)) == 0 })
	waitFor(t, func() bool {
		ups := r.approvalUpdates()
		return len(ups) == 1 && ups[0].thread == "1900.0001" && strings.Contains(ups[0].text, "Approved")
	})
	waitFor(t, func() bool { return r.typed("approved") })
	if !denied(r.hook(t, "git push --force")) {
		t.Fatal("a different input ran")
	}
	if reply := r.hook(t, "git push"); denied(reply) {
		t.Fatalf("the approved push was denied: %s", reply.Output)
	}
	if !denied(r.hook(t, "git push")) {
		t.Fatal("the approval was used two times")
	}
	waitFor(t, func() bool {
		for _, s := range r.store.approvals() {
			if s.ID == a.ID {
				return s.State == domain.ApprovalUsed
			}
		}
		return false
	})
}

func TestMateDenyOfTheOwnerTellsTheSessionNo(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	waitFor(t, func() bool { return len(r.asks()) == 1 })
	r.click(mateOwner, domain.ApprovalDenyAction, a.ID)
	waitFor(t, func() bool {
		ups := r.approvalUpdates()
		return len(ups) == 1 && strings.Contains(ups[0].text, "Denied")
	})
	waitFor(t, func() bool { return r.typed("said no") })
	if !denied(r.hook(t, "git push")) {
		t.Fatal("a denied push ran")
	}
}

func TestApprovalDecideFromTheSocketActsAsTheOwner(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	var got domain.Approval
	if err := r.c.Call(context.Background(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: a.ID, Approve: true}, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.State != domain.ApprovalGranted {
		t.Fatalf("got %+v", got)
	}
	if err := r.c.Call(context.Background(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: a.ID, Approve: false}, nil); errCode(err) != rpc.CodeStale {
		t.Fatalf("second decide: %v", err)
	}
	if err := r.c.Call(context.Background(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: "nope", Approve: true}, nil); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("unknown id: %v", err)
	}
	waitFor(t, func() bool { return r.typed("approved") })
	if denied(r.hook(t, "git push")) {
		t.Fatal("the approved push was denied")
	}
}

func TestMateApprovalThatExpiredCannotBeUsed(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	r.clock.add(domain.ApprovalTTL + time.Minute)
	if got := pendingApprovals(t, r.c); len(got) != 0 {
		t.Fatalf("an expired approval is listed: %+v", got)
	}
	if err := r.c.Call(context.Background(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: a.ID, Approve: true}, nil); errCode(err) != rpc.CodeStale {
		t.Fatalf("decide of an expired approval: %v", err)
	}
	r.click(mateOwner, domain.ApprovalApproveAction, a.ID)
	time.Sleep(50 * time.Millisecond)
	if !denied(r.hook(t, "git push")) {
		t.Fatal("an expired approval let the push run")
	}
}

func TestMateApprovalGrantedButUnusedExpires(t *testing.T) {
	r := startApprovalRig(t, &memStore{})
	a := r.pendingOne(t)
	if err := r.c.Call(context.Background(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: a.ID, Approve: true}, nil); err != nil {
		t.Fatal(err)
	}
	r.clock.add(domain.ApprovalTTL + time.Minute)
	if !denied(r.hook(t, "git push")) {
		t.Fatal("an old approval let the push run")
	}
}

func TestMateApprovalSurvivesARestart(t *testing.T) {
	store := &memStore{}
	at := time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)
	store.snap.Approvals = []domain.Approval{{ID: "k1", SessionID: "s1", Mate: "helper", Requester: mateGuest, Channel: "C0CHAN001", ThreadTS: "1700.0001", Message: "1900.0009",
		Action: domain.Action{Kind: domain.ActionPush, Tool: "Bash", Input: `{"command":"git push"}`}, At: at, State: domain.ApprovalPending}}
	r := startApprovalRig(t, store)
	got := pendingApprovals(t, r.c)
	if len(got) != 1 || got[0].ID != "k1" {
		t.Fatalf("approvals %+v", got)
	}
	r.hook(t, "git push --force")
	waitFor(t, func() bool { return len(pendingApprovals(t, r.c)) == 2 })
	if got := pendingApprovals(t, r.c); got[0].ID == got[1].ID {
		t.Fatalf("a new approval took a used ID: %+v", got)
	}
	r.click(mateOwner, domain.ApprovalApproveAction, "k1")
	waitFor(t, func() bool { ups := r.approvalUpdates(); return len(ups) == 1 && ups[0].thread == "1900.0009" })
}

func TestMateApprovalSendsAPush(t *testing.T) {
	store := &memStore{}
	sub := domain.PushSubscription{Endpoint: "https://push.example/phone", P256dh: pushKeys().P256dh, Auth: pushKeys().Auth}
	store.PutDevice(domain.Device{ID: "a2b3c4d5", Name: "phone", TokenHash: "abc", Push: &sub})
	push := newFakePushProvider()
	r := startApprovalRig(t, store, daemon.WithPush(push))
	a := r.pendingOne(t)
	select {
	case p := <-push.sent:
		if p.msg != domain.ApprovalPush(a) {
			t.Fatalf("push %+v", p.msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no push")
	}
}
