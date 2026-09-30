package domain

import (
	"testing"
	"time"
)

func TestNotifyBannerFor(t *testing.T) {
	cases := []struct {
		name    string
		session Session
		title   string
		effect  Effect
		want    Banner
		ok      bool
	}{
		{"permission", Session{Harness: HarnessClaude}, "fix login", notify(StatePermission), Banner{Title: "fix login", Body: "needs permission", State: StatePermission}, true},
		{"waiting", Session{Harness: HarnessCodex}, "fix login", notify(StateWaiting), Banner{Title: "fix login", Body: "waiting", State: StateWaiting}, true},
		{"done", Session{Harness: HarnessClaude}, "fix login", notify(StateDone), Banner{Title: "fix login", Body: "done", State: StateDone}, true},
		{"muted", Session{Muted: true}, "fix login", notify(StateDone), Banner{}, false},
		{"not a notify effect", Session{}, "fix login", markUnread, Banner{}, false},
		{"unnamed falls back to the harness", Session{Harness: HarnessCodex}, "  ", notify(StateDone), Banner{Title: "codex", Body: "done", State: StateDone}, true},
	}
	for _, tt := range cases {
		got, ok := BannerFor(tt.session, tt.title, tt.effect)
		if ok != tt.ok || got != tt.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNotifyMutedSessionStillGoesUnread(t *testing.T) {
	s := Session{State: StateRunning, Muted: true}
	next, effects := s.Apply(HarnessEvent{Kind: EventStop})
	if !next.Unread {
		t.Fatal("muted session is not unread after done")
	}
	for _, e := range effects {
		if _, ok := BannerFor(next, "x", e); ok {
			t.Fatalf("muted session produced a banner for %+v", e)
		}
	}
}

func TestNotifySetMutedOnlyChangesMuted(t *testing.T) {
	s := Session{ID: "a", State: StateWaiting, Unread: true}
	got := s.SetMuted(true)
	if got.ID != "a" || got.State != StateWaiting || !got.Unread || !got.Muted {
		t.Fatalf("got %+v", got)
	}
	if got.SetMuted(false).Muted {
		t.Fatal("unmute kept Muted")
	}
}

func TestNotifyCoalescerAllowsOneBannerPerSessionPerWindow(t *testing.T) {
	c := NewCoalescer()
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	steps := []struct {
		session string
		at      time.Duration
		want    bool
	}{
		{"a", 0, true},
		{"a", time.Second, false},
		{"a", CoalesceWindow - time.Nanosecond, false},
		{"b", time.Second, true},
		{"a", CoalesceWindow, true},
		{"a", CoalesceWindow + time.Second, false},
	}
	for i, st := range steps {
		if got := c.Allow(st.session, t0.Add(st.at)); got != st.want {
			t.Fatalf("step %d (%s at %v): got %v", i, st.session, st.at, got)
		}
	}
}

func TestNotifyBurstOfTwentyAttentionEventsIsOneBanner(t *testing.T) {
	c := NewCoalescer()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	s := Session{ID: "a", State: StateRunning}
	banners := 0
	for i := range 20 {
		var effects []Effect
		s, effects = s.Apply(HarnessEvent{Kind: EventPermissionRequest})
		for _, e := range effects {
			if _, ok := BannerFor(s, "x", e); ok && c.Allow(s.ID, now.Add(time.Duration(i)*100*time.Millisecond)) {
				banners++
			}
		}
		s, _ = s.Apply(HarnessEvent{Kind: EventPostToolUse})
	}
	if banners != 1 {
		t.Fatalf("banners = %d, want 1", banners)
	}
}

// proof: throwaway
var _ = 0
