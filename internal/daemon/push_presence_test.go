package daemon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const presenceEvery = 5 * time.Millisecond

type presenceRig struct {
	*pushRig
	activity *fakeActivity
}

func newPresenceRig(t *testing.T, awayAfter time.Duration, lastInput time.Time) *presenceRig {
	t.Helper()
	r := &presenceRig{
		pushRig:  &pushRig{attentionRig: &attentionRig{n: newFakeNotifier(), fg: &fakeForeground{}}, push: newFakePushProvider(), store: &memStore{}},
		activity: newFakeActivity(lastInput),
	}
	r.d, r.path = start(t, r.store,
		daemon.WithNotifier(r.n, r.fg, nil),
		daemon.WithPush(r.push),
		daemon.WithPresence(r.activity, awayAfter, presenceEvery))
	r.c = dial(t, r.path)
	var err error
	if r.sub, err = r.c.Subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.applied(t)
	return r
}

func (r *presenceRig) applied(t *testing.T) {
	t.Helper()
	select {
	case <-r.activity.sampled:
	default:
	}
	for range 3 {
		select {
		case <-r.activity.sampled:
		case <-time.After(2 * time.Second):
			t.Fatal("the daemon stopped sampling terminal activity")
		}
	}
}

func (r *presenceRig) setActivity(t *testing.T, at time.Time) {
	t.Helper()
	r.activity.set(at)
	r.applied(t)
}

func (r *presenceRig) noPushFor(t *testing.T) {
	t.Helper()
	r.applied(t)
	r.noMorePushes(t)
}

func TestPushIsHeldWhileTheOwnerTypesAtTheTerminal(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Now())
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.noPushFor(t)
}

func TestPushGoesOutWhileTheOwnerIsAway(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Now().Add(-10*time.Minute))
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.pushes(t, 1); got[0].msg.Tag != "s1" {
		t.Fatalf("pushed %+v", got)
	}
}

func TestPushGoesOutWhenTheLastInputIsJustPastTheWindow(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Now().Add(-2*time.Minute-time.Second))
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	if got := r.pushes(t, 1); got[0].msg.Tag != "s1" {
		t.Fatalf("pushed %+v", got)
	}
}

func TestPushGoesOutWithNoClientAttached(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Time{})
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushHeldAtTheTerminalIsSentOnceWhenTheOwnerLeaves(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Now())
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.noPushFor(t)
	r.setActivity(t, time.Now().Add(-3*time.Minute))
	got := r.pushes(t, 1)
	if got[0].msg.Tag != "s1" || got[0].msg.Body != "needs permission" || got[0].msg.URL != "/#/sessions/s1" {
		t.Fatalf("released %+v", got[0].msg)
	}
	r.noPushFor(t)
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushHeldAtTheTerminalIsDroppedWhenTheSessionWasAnswered(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Now())
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.hook(t, "claude", "PostToolUse", "%1", "")
	r.setActivity(t, time.Now().Add(-3*time.Minute))
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushPresenceIsOffWithAZeroWindow(t *testing.T) {
	r := newPresenceRig(t, 0, time.Now())
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushSkipsADeviceWhoseAppIsOnScreen(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Time{})
	phone := r.pairedDevice(t, "phone", "https://push.example/phone")
	r.pairedDevice(t, "ipad", "https://push.example/ipad")
	stream := dial(t, r.path)
	viewing := func(visible bool) {
		t.Helper()
		if err := stream.Call(context.Background(), rpc.MethodDeviceViewing, rpc.DeviceViewingParams{Device: phone, Visible: visible}, nil); err != nil {
			t.Fatal(err)
		}
	}
	viewing(true)
	r.sentinelPushes(t, "https://push.example/ipad")
	viewing(false)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	r.pushes(t, 2)
	viewing(true)
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; ; i++ {
		id := fmt.Sprintf("c%d", i)
		r.addRunning(t, id, domain.HarnessClaude, "%"+id)
		r.hook(t, "claude", "Stop", "%"+id, "")
		r.pushes(t, 1)
		select {
		case <-r.push.sent:
			return
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the phone stayed viewing after its stream closed")
		}
	}
}

func clientViewing(t *testing.T, c *rpc.Client, session string, front bool) {
	t.Helper()
	if err := c.Call(context.Background(), rpc.MethodClientViewing, rpc.ClientViewingParams{Session: session, Front: front}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestClientViewingSuppressesTheBannerOfTheSessionInViewOnly(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	r.addRunning(t, "b", domain.HarnessClaude, "%2")
	app := dial(t, r.path)
	clientViewing(t, app, "a", true)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.hook(t, "claude", "PermissionRequest", "%2", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("banner %+v", got)
	}
	r.expectOnlySentinel(t)
}

func TestClientViewingStopsSuppressingInTheBackgroundOrOnClose(t *testing.T) {
	r := newRig(t, &memStore{}, nil)
	r.addRunning(t, "a", domain.HarnessClaude, "%1")
	app := dial(t, r.path)
	clientViewing(t, app, "a", true)
	clientViewing(t, app, "a", false)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.banner(t); got.State != domain.StatePermission {
		t.Fatalf("background banner %+v", got)
	}
	r.addRunning(t, "b", domain.HarnessClaude, "%2")
	other := dial(t, r.path)
	clientViewing(t, other, "b", true)
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.hook(t, "claude", "PermissionRequest", "%2", "")
		select {
		case b := <-r.n.banners:
			if b.State == domain.StatePermission {
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the closed client kept suppressing")
		}
		r.hook(t, "claude", "PostToolUse", "%2", "")
	}
}

func TestPushHeldWhileTheAppIsInFrontGoesOutAfterItLeavesAndTheWindowPasses(t *testing.T) {
	r := newPresenceRig(t, 50*time.Millisecond, time.Time{})
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	app := dial(t, r.path)
	clientViewing(t, app, "", true)
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	time.Sleep(100 * time.Millisecond)
	r.noPushFor(t)
	clientViewing(t, app, "", false)
	got := r.pushes(t, 1)
	if got[0].msg.Tag != "s1" {
		t.Fatalf("released %+v", got[0].msg)
	}
}

func TestDeviceViewingNeedsAKnownDevice(t *testing.T) {
	r := newPresenceRig(t, 2*time.Minute, time.Time{})
	err := r.c.Call(context.Background(), rpc.MethodDeviceViewing, rpc.DeviceViewingParams{Device: "nosuchid", Visible: true}, nil)
	if errCode(err) != rpc.CodeNotFound {
		t.Fatalf("unknown device: %v", err)
	}
}

func TestPushAwayAfterComesFromThePushTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if got, err := daemon.LoadAwayAfter(path); err != nil || got != domain.DefaultAwayAfter {
		t.Fatalf("missing file: %v, %v", got, err)
	}
	cases := []struct {
		toml string
		want time.Duration
		ok   bool
	}{
		{"[ui]\nmouse = false\n", domain.DefaultAwayAfter, true},
		{"[push]\naway_after = \"5m\"\n", 5 * time.Minute, true},
		{"[push]\naway_after = \"90s\"\n", 90 * time.Second, true},
		{"[push]\naway_after = \"0\"\n", 0, true},
		{"[push]\naway_after = \"soon\"\n", 0, false},
		{"[push]\naway_after = \"-1m\"\n", 0, false},
		{"[push\n", 0, false},
	}
	for _, c := range cases {
		if err := os.WriteFile(path, []byte(c.toml), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := daemon.LoadAwayAfter(path)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("%q: got %v, %v; want %v ok %v", c.toml, got, err, c.want, c.ok)
		}
	}
}
