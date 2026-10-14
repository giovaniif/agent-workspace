package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func (r *attentionRig) notices(t *testing.T) <-chan rpc.Notice {
	t.Helper()
	ch, err := dial(t, r.path).StreamNotices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func nextNotice(t *testing.T, ch <-chan rpc.Notice) rpc.Notice {
	t.Helper()
	select {
	case n, ok := <-ch:
		if !ok {
			t.Fatal("notice stream closed")
		}
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("no notice")
	}
	return rpc.Notice{}
}

func TestNotifyStreamRelaysBannersAndWithdrawals(t *testing.T) {
	r := newRig(t, &memStore{}, map[domain.AgentState]string{domain.StateDone: "Hero"})
	ch := r.notices(t)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	got := nextNotice(t, ch)
	if got.Banner == nil || got.Banner.Group != "s1" || got.Banner.State != domain.StateDone || got.Banner.Sound != "Hero" || got.Focused {
		t.Fatalf("banner notice %+v", got)
	}
	if err := r.c.FocusSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if got := nextNotice(t, ch); got.Remove != "s1" || got.Banner != nil {
		t.Fatalf("removal notice %+v", got)
	}
}

func TestNotifyStreamMarksAFocusedSessionsBanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	ch := r.notices(t)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	if err := r.c.FocusSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "Stop", "%1", "")
	if got := nextNotice(t, ch); got.Banner == nil || !got.Focused {
		t.Fatalf("notice %+v", got)
	}
}

func TestNotifyStreamSkipsMutedSessions(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	ch := r.notices(t)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "m", Harness: domain.HarnessClaude, Pane: "%m", State: domain.StateRunning, Muted: true}})
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "Stop", "%m", "")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	if got := nextNotice(t, ch); got.Banner == nil || got.Banner.Group != "s1" {
		t.Fatalf("notice %+v", got)
	}
}

func TestNotifyStreamEndsWhenItsContextIsCancelled(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := dial(t, r.path).StreamNotices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("a notice arrived after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream stayed open after its context was cancelled")
	}
}
