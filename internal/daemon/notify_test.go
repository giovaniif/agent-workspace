package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type attentionRig struct {
	c    *rpc.Client
	d    *daemon.Daemon
	sub  rpc.Subscription
	n    *fakeNotifier
	fg   *fakeForeground
	path string
}

func newRig(t *testing.T, store app.Store, sounds map[domain.AgentState]string) *attentionRig {
	t.Helper()
	r := &attentionRig{n: newFakeNotifier(), fg: &fakeForeground{}}
	r.d, r.path = start(t, store, daemon.WithNotifier(r.n, r.fg, sounds))
	r.c = dial(t, r.path)
	var err error
	if r.sub, err = r.c.Subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *attentionRig) addRunning(t *testing.T, id string, harness domain.Harness, pane string) {
	t.Helper()
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: id, Harness: harness, Pane: pane, State: domain.StateRunning}})
	next(t, r.sub.Diffs)
}

func (r *attentionRig) hook(t *testing.T, harness, event, pane, payload string) rpc.Diff {
	t.Helper()
	h := rpc.Hook{Harness: harness, Event: event, Pane: pane}
	if payload != "" {
		h.Payload = json.RawMessage(payload)
	}
	if err := r.c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
	return next(t, r.sub.Diffs)
}

func (r *attentionRig) banner(t *testing.T) domain.Banner {
	t.Helper()
	select {
	case b := <-r.n.banners:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("no banner")
	}
	return domain.Banner{}
}

func (r *attentionRig) sentinel(t *testing.T) {
	t.Helper()
	r.addRunning(t, "sentinel", domain.HarnessClaude, "%sentinel")
	r.hook(t, "claude", "Stop", "%sentinel", "")
}

func (r *attentionRig) expectOnlySentinel(t *testing.T) {
	t.Helper()
	r.sentinel(t)
	if b := r.banner(t); b.Title != string(domain.HarnessClaude) || b.State != domain.StateDone {
		t.Fatalf("expected the sentinel banner, got %+v", b)
	}
	select {
	case b := <-r.n.banners:
		t.Fatalf("extra banner %+v", b)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClaudeAndCodexNotifyOnTheSameEvents(t *testing.T) {
	fixtures := []struct {
		harness string
		event   string
		payload string
		want    domain.AgentState
		body    string
	}{
		{"claude", "Notification", `{"notification_type":"permission_prompt"}`, domain.StatePermission, "needs permission"},
		{"claude", "PermissionRequest", "", domain.StatePermission, "needs permission"},
		{"claude", "Notification", `{"notification_type":"idle_prompt"}`, domain.StateWaiting, "waiting"},
		{"claude", "Stop", "", domain.StateDone, "done"},
		{"codex", "PermissionRequest", "", domain.StatePermission, "needs permission"},
		{"codex", "Stop", "", domain.StateDone, "done"},
	}
	for _, f := range fixtures {
		r := newRig(t, &memStore{}, nil)
		r.addRunning(t, "a", domain.Harness(f.harness), "%1")
		r.hook(t, f.harness, f.event, "%1", f.payload)
		got := r.banner(t)
		if got.State != f.want || got.Body != f.body || got.Title != f.harness {
			t.Errorf("%s %s: banner %+v", f.harness, f.event, got)
		}
	}
}

func TestNotifyBannerTitleIsTheSessionName(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.d.Post(daemon.TaskChanged{Task: domain.Task{ID: "t1", Text: "fix login", PinnedName: "login flow"}})
	next(t, r.sub.Diffs)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", TaskID: "t1", Harness: domain.HarnessCodex, Pane: "%1", State: domain.StateRunning}})
	next(t, r.sub.Diffs)
	r.hook(t, "codex", "Stop", "%1", "")
	if got := r.banner(t); got.Title != "login flow" {
		t.Fatalf("title %q", got.Title)
	}
}

func TestNotifySoundComesFromTheEventType(t *testing.T) {
	r := newRig(t, &memStore{}, map[domain.AgentState]string{domain.StatePermission: "Glass"})
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.Sound != "Glass" {
		t.Fatalf("permission sound %q", got.Sound)
	}
	r.addRunning(t, "b", domain.HarnessClaude, "%2")
	r.hook(t, "claude", "Stop", "%2", "")
	if got := r.banner(t); got.Sound != "" {
		t.Fatalf("done sound %q", got.Sound)
	}
}

func TestNotifyBannerCarriesRepoAndWhatTheHookAsked(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: "w1", Repo: "api", Branch: "42-retry", Path: "/w/api"}})
	next(t, r.sub.Diffs)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%1", State: domain.StateRunning, WorktreeIDs: []string{"w1"}}})
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "PermissionRequest", "%1", `{"tool_name":"Bash","tool_input":{"command":"npm test"}}`)
	got := r.banner(t)
	if got.Title != "claude · api@42-retry" || got.Body != "needs permission: Bash: npm test" {
		t.Fatalf("banner %q / %q", got.Title, got.Body)
	}
}

func TestNotifyMutedSessionGetsNoBannerButGoesUnread(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	if err := r.c.Call(context.Background(), rpc.MethodSessionMute, rpc.SessionMuteParams{ID: "a", Muted: true}, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, r.sub.Diffs); diff.Session == nil || !diff.Session.Muted {
		t.Fatalf("mute diff %+v", diff)
	}
	diff := r.hook(t, "claude", "Stop", "%1", "")
	if diff.Session == nil || !diff.Session.Unread || !diff.Session.Muted {
		t.Fatalf("stop diff %+v", diff.Session)
	}
	r.expectOnlySentinel(t)
}

func TestNotifyUnmuteRestoresBanners(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	for _, muted := range []bool{true, false} {
		if err := r.c.Call(context.Background(), rpc.MethodSessionMute, rpc.SessionMuteParams{ID: "a", Muted: muted}, nil); err != nil {
			t.Fatal(err)
		}
		next(t, r.sub.Diffs)
	}
	r.hook(t, "claude", "Stop", "%1", "")
	if got := r.banner(t); got.State != domain.StateDone {
		t.Fatalf("banner %+v", got)
	}
}

func TestNotifyBurstOfTwentyEventsIsOneBanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("banner %+v", got)
	}
	r.hook(t, "claude", "PostToolUse", "%1", "")
	for range 19 {
		r.hook(t, "claude", "PermissionRequest", "%1", "")
		r.hook(t, "claude", "PostToolUse", "%1", "")
	}
	r.expectOnlySentinel(t)
}

func TestNotifyFocusedSessionInAFrontTerminalGetsNoBanner(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, r.sub.Diffs); diff.Session == nil || !diff.Session.Focused {
		t.Fatalf("focus diff %+v", diff)
	}
	r.fg.terminal.Store(true)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.expectOnlySentinel(t)
}

func TestNotifyFocusedSessionStillNotifiesWhenAnotherAppIsInFront(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("banner %+v", got)
	}
}

func TestNotifyUnfocusedSessionNotifiesEvenWithTheTerminalInFront(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	r.fg.terminal.Store(true)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("banner %+v", got)
	}
}

func TestNotifyFocusingClearsUnreadAndBlursTheOtherSession(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	r.addRunning(t, "b", domain.HarnessClaude, "%2")
	focus := func(id string, diffs int) []domain.Session {
		if err := r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: id}, nil); err != nil {
			t.Fatal(err)
		}
		var out []domain.Session
		for range diffs {
			if diff := next(t, r.sub.Diffs); diff.Session != nil {
				out = append(out, *diff.Session)
			}
		}
		return out
	}
	focus("a", 1)
	if diff := r.hook(t, "claude", "Stop", "%2", ""); diff.Session == nil || !diff.Session.Unread {
		t.Fatalf("b not unread: %+v", diff.Session)
	}
	got := focus("b", 2)
	byID := map[string]domain.Session{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if !byID["b"].Focused || byID["b"].Unread || byID["a"].Focused {
		t.Fatalf("after focusing b: %+v", byID)
	}
}

func TestNotifyMuteAndFocusUnknownSessionAreNotFound(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	var rerr *rpc.Error
	err := r.c.Call(context.Background(), rpc.MethodSessionMute, rpc.SessionMuteParams{ID: "nope", Muted: true}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("mute: %v", err)
	}
	err = r.c.Call(context.Background(), rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: "nope"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("focus: %v", err)
	}
}

func TestNotifyFocusFromBeforeARestartDoesNotSuppressBanners(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "a", Harness: domain.HarnessClaude, Pane: "%1", State: domain.StateRunning, Focused: true}}
	r := newRig(t, store, nil)
	r.fg.terminal.Store(true)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("banner %+v", got)
	}
}
