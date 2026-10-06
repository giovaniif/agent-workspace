package domain

import (
	"reflect"
	"testing"
	"time"
)

var presenceNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func TestPresenceAtTerminalWhileTheLastInputIsInsideTheWindow(t *testing.T) {
	cases := []struct {
		name string
		p    Presence
		want bool
	}{
		{"typed just now", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow}, true},
		{"typed a minute ago", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-time.Minute)}, true},
		{"a second inside the window", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-2*time.Minute + time.Second)}, true},
		{"exactly the window ago", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-2 * time.Minute)}, false},
		{"just past the window", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-2*time.Minute - time.Second)}, false},
		{"no client attached", Presence{AwayAfter: 2 * time.Minute}, false},
		{"suppression off", Presence{AwayAfter: 0, LastInput: presenceNow}, false},
		{"a negative window", Presence{AwayAfter: -time.Minute, LastInput: presenceNow}, false},
		{"a clock a second behind tmux", Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(time.Second)}, true},
	}
	for _, c := range cases {
		if got := c.p.AtTerminal(presenceNow); got != c.want {
			t.Errorf("%s: AtTerminal = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNeedsYouIsPermissionWaitingOrAnUnreadDone(t *testing.T) {
	cases := []struct {
		s    Session
		want bool
	}{
		{Session{State: StatePermission}, true},
		{Session{State: StateWaiting}, true},
		{Session{State: StateDone, Unread: true}, true},
		{Session{State: StateDone}, false},
		{Session{State: StateRunning, Unread: true}, false},
		{Session{State: StateIdle}, false},
		{Session{State: StatePermission, Muted: true}, false},
		{Session{State: StateWaiting, Ended: true}, false},
	}
	for _, c := range cases {
		if got := NeedsYou(c.s); got != c.want {
			t.Errorf("%+v: NeedsYou = %v, want %v", c.s, got, c.want)
		}
	}
}

func heldBanner(id string, state AgentState) Banner {
	return Banner{Title: id, Body: string(state), State: state, Group: id}
}

var (
	present = Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-10 * time.Second)}
	away    = Presence{AwayAfter: 2 * time.Minute, LastInput: presenceNow.Add(-3 * time.Minute)}
)

func TestPushGateHoldsWhilePresentAndSendsWhileAway(t *testing.T) {
	g := NewPushGate()
	if g.Admit(heldBanner("s1", StatePermission), present, presenceNow) {
		t.Fatal("a push went out while the owner was at the terminal")
	}
	if !g.Admit(heldBanner("s2", StatePermission), away, presenceNow) {
		t.Fatal("a push was held while the owner was away")
	}
	off := Presence{LastInput: presenceNow}
	if !g.Admit(heldBanner("s3", StateDone), off, presenceNow) {
		t.Fatal("a push was held with suppression off")
	}
}

func TestPushGateReleasesAHeldPushOnceWhenTheOwnerLeaves(t *testing.T) {
	g := NewPushGate()
	g.Admit(heldBanner("s1", StatePermission), present, presenceNow)
	sessions := []Session{{ID: "s1", State: StatePermission}}
	if got := g.Release(sessions, present, presenceNow); len(got) != 0 {
		t.Fatalf("released %+v while the owner was still there", got)
	}
	later := presenceNow.Add(3 * time.Minute)
	want := []Banner{heldBanner("s1", StatePermission)}
	if got := g.Release(sessions, present, later); !reflect.DeepEqual(got, want) {
		t.Fatalf("released %+v, want %+v", got, want)
	}
	if got := g.Release(sessions, present, later); len(got) != 0 {
		t.Fatalf("released %+v a second time", got)
	}
}

func TestPushGateReleasesOnlySessionsThatStillNeedYou(t *testing.T) {
	g := NewPushGate()
	for _, b := range []Banner{
		heldBanner("answered", StatePermission),
		heldBanner("read", StateDone),
		heldBanner("muted", StateWaiting),
		heldBanner("gone", StatePermission),
		heldBanner("asks", StateWaiting),
		heldBanner("unread", StateDone),
	} {
		g.Admit(b, present, presenceNow)
	}
	sessions := []Session{
		{ID: "answered", State: StateRunning},
		{ID: "read", State: StateDone},
		{ID: "muted", State: StateWaiting, Muted: true},
		{ID: "asks", State: StateWaiting},
		{ID: "unread", State: StateDone, Unread: true},
	}
	want := []Banner{heldBanner("asks", StateWaiting), heldBanner("unread", StateDone)}
	if got := g.Release(sessions, away, presenceNow); !reflect.DeepEqual(got, want) {
		t.Fatalf("released %+v, want %+v", got, want)
	}
	if got := g.Release(sessions, away, presenceNow); len(got) != 0 {
		t.Fatalf("released %+v a second time", got)
	}
}

func TestPushGateKeepsTheNewestHeldBannerPerSession(t *testing.T) {
	g := NewPushGate()
	g.Admit(heldBanner("s1", StatePermission), present, presenceNow)
	g.Admit(heldBanner("s1", StateDone), present, presenceNow)
	sessions := []Session{{ID: "s1", State: StateDone, Unread: true}}
	want := []Banner{heldBanner("s1", StateDone)}
	if got := g.Release(sessions, away, presenceNow); !reflect.DeepEqual(got, want) {
		t.Fatalf("released %+v, want %+v", got, want)
	}
}

func TestPushGateDropsAHeldPushOnceANewerOneWentOut(t *testing.T) {
	g := NewPushGate()
	g.Admit(heldBanner("s1", StatePermission), present, presenceNow)
	if !g.Admit(heldBanner("s1", StateWaiting), away, presenceNow) {
		t.Fatal("the newer push was held while away")
	}
	sessions := []Session{{ID: "s1", State: StateWaiting}}
	if got := g.Release(sessions, away, presenceNow); len(got) != 0 {
		t.Fatalf("released %+v after a newer push already went out", got)
	}
}

func TestViewingIsTheDevicesWithAFreshVisibleReport(t *testing.T) {
	reports := []ViewReport{
		{Device: "phone", Visible: true, At: presenceNow.Add(-time.Second)},
		{Device: "ipad", Visible: false, At: presenceNow},
		{Device: "stale", Visible: true, At: presenceNow.Add(-ViewingFresh)},
		{Device: "edge", Visible: true, At: presenceNow.Add(-ViewingFresh + time.Second)},
		{Device: "twice", Visible: false, At: presenceNow},
		{Device: "twice", Visible: true, At: presenceNow},
	}
	want := map[string]bool{"phone": true, "edge": true, "twice": true}
	if got := Viewing(reports, presenceNow); !reflect.DeepEqual(got, want) {
		t.Fatalf("viewing %v, want %v", got, want)
	}
	if got := Viewing(nil, presenceNow); len(got) != 0 {
		t.Fatalf("no reports gave %v", got)
	}
}
