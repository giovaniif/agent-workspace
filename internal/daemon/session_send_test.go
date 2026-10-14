package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func sendText(t *testing.T, c *rpc.Client, text string) rpc.SessionSent {
	t.Helper()
	sent, err := c.SessionSend(context.Background(), "s1", text)
	if err != nil {
		t.Fatal(err)
	}
	return sent
}

func pasted(text string) []string {
	return []string{"%1 paste=true " + text, "%1 keys Enter"}
}

func queuedTexts(t *testing.T, c *rpc.Client) []string {
	t.Helper()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, q := range sub.State.Sends {
		out = append(out, q.Text)
	}
	return out
}

func assertTypedStays(t *testing.T, env sendEnv, want []string) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	if typed := env.host.typedNow(); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q, want %q", typed, want)
	}
}

func TestSessionSendToAnIdleSessionPastesAtOnce(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	sent := sendText(t, env.c, "go on")
	if sent.Queued || sent.ID == "" {
		t.Fatalf("sent %+v, want pasted at once with an id", sent)
	}
	if typed := env.host.waitTyped(t, 2); !reflect.DeepEqual(typed, pasted("go on")) {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionSendToAWorkingSessionIsQueuedUntilTheNextStop(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	sent := sendText(t, env.c, "then run the tests")
	if !sent.Queued || sent.ID == "" {
		t.Fatalf("sent %+v, want queued with an id", sent)
	}
	assertTypedStays(t, env, nil)
	if got := queuedTexts(t, env.c); !reflect.DeepEqual(got, []string{"then run the tests"}) {
		t.Fatalf("State.Sends texts %q", got)
	}
	hook(t, env.c, "Stop")
	if typed := env.host.waitTyped(t, 2); !reflect.DeepEqual(typed, pasted("then run the tests")) {
		t.Fatalf("typed %q", typed)
	}
	waitUntil(t, "queue emptied", func() bool { return len(queuedTexts(t, env.c)) == 0 })
}

func TestSessionSendTwoSendsToABusySessionGoOutInOrderOnePerFreeTurn(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	sendText(t, env.c, "first")
	sendText(t, env.c, "second")
	hook(t, env.c, "Stop")
	env.host.waitTyped(t, 2)
	assertTypedStays(t, env, pasted("first"))
	hook(t, env.c, "UserPromptSubmit")
	assertTypedStays(t, env, pasted("first"))
	hook(t, env.c, "Stop")
	want := append(pasted("first"), pasted("second")...)
	if typed := env.host.waitTyped(t, 4); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionSendDroppedQueuedSendIsNeverPasted(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	dropped := sendText(t, env.c, "never")
	sendText(t, env.c, "kept")
	if err := env.c.SessionUnsend(context.Background(), "s1", dropped.ID); err != nil {
		t.Fatal(err)
	}
	if got := queuedTexts(t, env.c); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("State.Sends texts %q", got)
	}
	hook(t, env.c, "Stop")
	env.host.waitTyped(t, 2)
	assertTypedStays(t, env, pasted("kept"))
	var rerr *rpc.Error
	if err := env.c.SessionUnsend(context.Background(), "s1", dropped.ID); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("dropping it again: %v, want not_found", err)
	}
}

func TestSessionSendQueueReachesSubscribersAsDiffs(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	sub, err := dial(t, env.path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sent := sendText(t, env.c, "later")
	want := []domain.QueuedSend{{ID: sent.ID, Session: "s1", Text: "later"}}
	waitSendsDiff(t, sub, func(q []domain.QueuedSend) bool {
		return len(q) == 1 && q[0].ID == want[0].ID && q[0].Session == "s1" && q[0].Text == "later" && !q[0].QueuedAt.IsZero()
	})
	hook(t, env.c, "Stop")
	waitSendsDiff(t, sub, func(q []domain.QueuedSend) bool { return len(q) == 0 })
}

func waitSendsDiff(t *testing.T, sub rpc.Subscription, match func([]domain.QueuedSend) bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case d := <-sub.Diffs:
			if d.Sends != nil && match(*d.Sends) {
				return
			}
		case <-deadline:
			t.Fatal("no matching sends diff")
		}
	}
}

func TestSessionSendThatFailsToPasteStaysQueued(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	env.host.failOn("flaky")
	sendText(t, env.c, "flaky")
	waitUntil(t, "requeued", func() bool { return reflect.DeepEqual(queuedTexts(t, env.c), []string{"flaky"}) })
}

func TestSessionSendThatFailsToPasteFreesAReviewDraftWaitingBehindIt(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	env.host.failOn("flaky")
	release := env.host.holdPane("%1")
	sendText(t, env.c, "flaky")
	env.host.waitHeld(t, "%1")
	addComments(t, env.c, commentA)
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	release()
	want := pasted(domain.ReviewPrompt([]domain.ReviewComment{commentA}))
	if typed := env.host.waitTyped(t, 2); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionSendAndAQueuedReviewDraftTakeSeparateTurns(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	addComments(t, env.c, commentA)
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	sendText(t, env.c, "and then this")
	hook(t, env.c, "Stop")
	draft := pasted(domain.ReviewPrompt([]domain.ReviewComment{commentA}))
	env.host.waitTyped(t, 2)
	assertTypedStays(t, env, draft)
	hook(t, env.c, "UserPromptSubmit")
	hook(t, env.c, "Stop")
	if typed := env.host.waitTyped(t, 4); !reflect.DeepEqual(typed, append(draft, pasted("and then this")...)) {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionSendQueueIsDroppedWhenTheSessionEnds(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	sendText(t, env.c, "stale")
	if err := env.c.Call(context.Background(), rpc.MethodEndSession, rpc.SessionRef{ID: "s1"}, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "queue dropped", func() bool { return len(queuedTexts(t, env.c)) == 0 })
	var rerr *rpc.Error
	if _, err := env.c.SessionSend(context.Background(), "s1", "hello"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeFailed {
		t.Errorf("send to an ended session: %v, want failed", err)
	}
}

func TestSessionSendRefusesEmptyTextAndAnUnknownSession(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	var rerr *rpc.Error
	if _, err := env.c.SessionSend(context.Background(), "s1", "  \n"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("empty text: %v, want bad_request", err)
	}
	if _, err := env.c.SessionSend(context.Background(), "nope", "hi"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v, want not_found", err)
	}
	assertTypedStays(t, env, nil)
}

func TestSessionInterruptSendsEscapeToThePane(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	if err := env.c.SessionInterrupt(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if typed := env.host.typedNow(); !reflect.DeepEqual(typed, []string{"%1 keys Escape"}) {
		t.Fatalf("typed %q", typed)
	}
	var rerr *rpc.Error
	if err := env.c.SessionInterrupt(context.Background(), "nope"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v, want not_found", err)
	}
}
