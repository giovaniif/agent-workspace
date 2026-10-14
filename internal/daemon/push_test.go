package daemon_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type pushRig struct {
	*attentionRig
	push  *fakePushProvider
	store *memStore
}

func newPushRig(t *testing.T, store *memStore) *pushRig {
	t.Helper()
	r := &pushRig{attentionRig: &attentionRig{n: newFakeNotifier(), fg: &fakeForeground{}}, push: newFakePushProvider(), store: store}
	r.d, r.path = start(t, store, daemon.WithNotifier(r.n, r.fg, nil), daemon.WithPush(r.push))
	r.c = dial(t, r.path)
	var err error
	if r.sub, err = r.c.Subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func pushKeys() rpc.PushKeys {
	p256dh := make([]byte, 65)
	p256dh[0] = 4
	return rpc.PushKeys{
		P256dh: base64.RawURLEncoding.EncodeToString(p256dh),
		Auth:   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
}

func (r *pushRig) subscribe(endpoint, device string) error {
	return r.c.Call(context.Background(), rpc.MethodPushSubscribe, rpc.PushSubscribeParams{Device: device, Endpoint: endpoint, Keys: pushKeys()}, nil)
}

func (r *pushRig) pairedDevice(t *testing.T, name, endpoint string) string {
	t.Helper()
	got, err := redeem(r.c, pairCode(t, r.c, name).Code, "", "100.64.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.subscribe(endpoint, got.Device.ID); err != nil {
		t.Fatal(err)
	}
	return got.Device.ID
}

func (r *pushRig) pushes(t *testing.T, n int) []pushed {
	t.Helper()
	var out []pushed
	for len(out) < n {
		select {
		case p := <-r.push.sent:
			out = append(out, p)
		case <-time.After(2 * time.Second):
			t.Fatalf("got %d of %d pushes: %+v", len(out), n, out)
		}
	}
	slices.SortFunc(out, func(a, b pushed) int {
		if a.endpoint < b.endpoint {
			return -1
		}
		if a.endpoint > b.endpoint {
			return 1
		}
		return 0
	})
	return out
}

func (r *pushRig) noMorePushes(t *testing.T) {
	t.Helper()
	select {
	case p := <-r.push.sent:
		t.Fatalf("extra push %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
}

func (r *pushRig) sentinelPushes(t *testing.T, endpoints ...string) {
	t.Helper()
	r.sentinel(t)
	got := r.pushes(t, len(endpoints))
	for i, p := range got {
		if p.msg.Tag != "sentinel" || p.endpoint != endpoints[i] {
			t.Fatalf("expected the sentinel's pushes to %v, got %+v", endpoints, got)
		}
	}
	r.noMorePushes(t)
}

func TestPushNotifiesEverySubscribedDeviceOnceWhenASessionNeedsPermission(t *testing.T) {
	r := newPushRig(t, &memStore{})
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.pairedDevice(t, "ipad", "https://push.example/ipad")
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	r.hook(t, "claude", "Notification", "%1", `{"notification_type":"permission_prompt"}`)
	got := r.pushes(t, 2)
	if got[0].endpoint != "https://push.example/ipad" || got[1].endpoint != "https://push.example/phone" {
		t.Fatalf("pushed to %+v", got)
	}
	for _, p := range got {
		if p.msg.Title != "claude" || p.msg.Body != "needs permission" || p.msg.URL != "/#/sessions/s1" || p.msg.Tag != "s1" {
			t.Fatalf("push %+v", p.msg)
		}
	}
	r.sentinelPushes(t, "https://push.example/ipad", "https://push.example/phone")
}

func TestPushSendsNothingForAMutedSession(t *testing.T) {
	r := newPushRig(t, &memStore{})
	r.pairedDevice(t, "phone", "https://push.example/phone")
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "m", Harness: domain.HarnessClaude, Pane: "%m", State: domain.StateRunning, Muted: true}})
	next(t, r.sub.Diffs)
	r.hook(t, "claude", "PermissionRequest", "%m", "")
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushReachesNoRevokedDevice(t *testing.T) {
	store := &memStore{}
	r := newPushRig(t, store)
	phone := r.pairedDevice(t, "phone", "https://push.example/phone")
	r.pairedDevice(t, "ipad", "https://push.example/ipad")
	if err := r.c.Call(context.Background(), rpc.MethodDeviceRevoke, rpc.DeviceRevokeParams{ID: phone}, nil); err != nil {
		t.Fatal(err)
	}
	next(t, r.sub.Diffs)
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "PermissionRequest", "%1", "")
	if got := r.pushes(t, 1); got[0].endpoint != "https://push.example/ipad" {
		t.Fatalf("pushed to %+v", got)
	}
	r.sentinelPushes(t, "https://push.example/ipad")
	for _, d := range store.devices() {
		if d.ID == phone {
			t.Fatalf("the revoked device is still stored with %+v", d.Push)
		}
	}
}

func TestPushDropsASubscriptionThePushServiceNoLongerHas(t *testing.T) {
	store := &memStore{}
	r := newPushRig(t, store)
	phone := r.pairedDevice(t, "phone", "https://push.example/phone")
	r.pairedDevice(t, "ipad", "https://push.example/ipad")
	r.push.fail("https://push.example/phone", fmt.Errorf("410 Gone: %w", app.ErrPushGone))
	r.addRunning(t, "s1", domain.HarnessClaude, "%1")
	r.hook(t, "claude", "Stop", "%1", "")
	r.pushes(t, 2)
	deadline := time.Now().Add(2 * time.Second)
	for {
		i := slices.IndexFunc(store.devices(), func(d domain.Device) bool { return d.ID == phone })
		if i >= 0 && store.devices()[i].Push == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the gone subscription is still stored: %+v", store.devices())
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.sentinelPushes(t, "https://push.example/ipad")
}

func TestPushReachesASubscriptionStoredBeforeARestart(t *testing.T) {
	store := &memStore{}
	sub := domain.PushSubscription{Endpoint: "https://push.example/phone", P256dh: pushKeys().P256dh, Auth: pushKeys().Auth}
	store.PutDevice(domain.Device{ID: "a2b3c4d5", Name: "phone", TokenHash: "abc", Push: &sub})
	r := newPushRig(t, store)
	r.sentinelPushes(t, "https://push.example/phone")
}

func TestPushSubscribeNeedsAKnownDeviceAndAUsableSubscription(t *testing.T) {
	r := newPushRig(t, &memStore{})
	got, err := redeem(r.c, pairCode(t, r.c, "phone").Code, "", "100.64.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.subscribe("https://push.example/x", "nosuchid"); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("unknown device: %v", err)
	}
	if err := r.subscribe("http://push.example/x", got.Device.ID); errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("plain http endpoint: %v", err)
	}
	if err := r.subscribe("https://push.example/x", got.Device.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPushKeyIsTheProvidersPublicKey(t *testing.T) {
	r := newPushRig(t, &memStore{})
	var key rpc.PushKey
	if err := r.c.Call(context.Background(), rpc.MethodPushKey, nil, &key); err != nil {
		t.Fatal(err)
	}
	if key.PublicKey != "BFakeVapidPublicKey" {
		t.Fatalf("key %+v", key)
	}
}

func TestPushMethodsAreUnknownWithoutAProvider(t *testing.T) {
	_, path := start(t, &memStore{})
	c := dial(t, path)
	if err := c.Call(context.Background(), rpc.MethodPushKey, nil, nil); errCode(err) != rpc.CodeUnknownMethod {
		t.Fatalf("push.key: %v", err)
	}
	if err := c.Call(context.Background(), rpc.MethodPushSubscribe, rpc.PushSubscribeParams{}, nil); errCode(err) != rpc.CodeUnknownMethod {
		t.Fatalf("push.subscribe: %v", err)
	}
}

func TestPushUnsubscribeStopsPushesToThatDevice(t *testing.T) {
	store := &memStore{}
	r := newPushRig(t, store)
	phone := r.pairedDevice(t, "phone", "https://push.example/phone")
	r.pairedDevice(t, "ipad", "https://push.example/ipad")
	if err := r.c.Call(context.Background(), rpc.MethodPushUnsubscribe, rpc.PushUnsubscribeParams{Device: phone}, nil); err != nil {
		t.Fatal(err)
	}
	r.sentinelPushes(t, "https://push.example/ipad")
	for _, d := range store.devices() {
		if d.ID == phone && d.Push != nil {
			t.Fatalf("the phone still stores %+v", d.Push)
		}
	}
	if err := r.c.Call(context.Background(), rpc.MethodPushUnsubscribe, rpc.PushUnsubscribeParams{Device: "nosuchid"}, nil); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("unknown device: %v", err)
	}
}
