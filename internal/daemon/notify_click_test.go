package daemon_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func (r *attentionRig) removal(t *testing.T) string {
	t.Helper()
	select {
	case g := <-r.n.removals:
		return g
	case <-time.After(2 * time.Second):
		t.Fatal("no removal")
	}
	return ""
}

func TestNotifyBannerIsGroupedBySession(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	if got := r.banner(t); got.Group != "s1" {
		t.Fatalf("group %q", got.Group)
	}
}

func TestNotifyBannerActivatesTheClientTerminal(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.d.SetClientHost(&fakeClientHost{})
	if _, err := r.c.OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"crew", "tui"}, Terminal: "com.apple.Terminal"}); err != nil {
		t.Fatal(err)
	}
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	if got := r.banner(t); got.Terminal != "com.apple.Terminal" {
		t.Fatalf("terminal %q", got.Terminal)
	}
}

func TestNotifyResumingWithdrawsTheBanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.banner(t)
	r.hook(t, "claude", "PreToolUse", "%1", `{"tool_name":"Bash"}`)
	if got := r.removal(t); got != "s1" {
		t.Fatalf("removed %q", got)
	}
}

func TestNotifyFocusingWithdrawsTheBanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	r.banner(t)
	if err := r.c.FocusSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if got := r.removal(t); got != "s1" {
		t.Fatalf("removed %q", got)
	}
}

func TestNotifyAttachFromAnUnknownTerminalActivatesNone(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.d.SetClientHost(&fakeClientHost{})
	tui := rpc.OpenClientParams{Command: []string{"crew", "tui"}, Terminal: "com.apple.Terminal"}
	if _, err := r.c.OpenClient(context.Background(), tui); err != nil {
		t.Fatal(err)
	}
	tui.Terminal = ""
	if _, err := r.c.OpenClient(context.Background(), tui); err != nil {
		t.Fatal(err)
	}
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	if got := r.banner(t); got.Terminal != "" {
		t.Fatalf("terminal %q", got.Terminal)
	}
}

func TestNotifyWithdrawalIsKeptWhileTheQueueIsFull(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	for i := range 140 {
		pane := fmt.Sprintf("%%%d", i)
		r.addRunning(t, fmt.Sprintf("s%d", i), domain.HarnessClaude, pane)
		r.hook(t, "claude", "Stop", pane, "")
	}
	if err := r.c.FocusSession(context.Background(), "s0"); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	go func() {
		for range r.n.banners {
		}
	}()
	if got := r.removal(t); got != "s0" {
		t.Fatalf("removed %q", got)
	}
}

func TestNotifyWithdrawnBannerStillQueuedIsNeverPosted(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	for i := range 66 {
		pane := fmt.Sprintf("%%%d", i)
		r.addRunning(t, fmt.Sprintf("s%d", i), domain.HarnessClaude, pane)
		r.hook(t, "claude", "Stop", pane, "")
	}
	if err := r.c.FocusSession(context.Background(), "s65"); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	seen := make(chan string, 128)
	go func() {
		for b := range r.n.banners {
			seen <- b.Group
		}
	}()
	if got := r.removal(t); got != "s65" {
		t.Fatalf("removed %q", got)
	}
	r.sentinel(t)
	for g := range seen {
		if g == "s65" {
			t.Fatal("the withdrawn banner was posted")
		}
		if g == "sentinel" {
			return
		}
	}
}

func TestNotifyNothingToWithdrawWithoutABanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	if err := r.c.FocusSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "PreToolUse", "%1", `{"tool_name":"Bash"}`)
	r.addRunning(t, "s2", domain.HarnessClaude, "%2")
	r.hook(t, "claude", "Stop", "%2", "")
	r.banner(t)
	select {
	case g := <-r.n.removals:
		t.Fatalf("removed %q with no banner up", g)
	default:
	}
}
