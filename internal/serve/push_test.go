package serve_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

const browserSubscription = `{"endpoint":"https://web.push.apple.com/QGx3","expirationTime":null,"keys":{"p256dh":"BNcRdreALRFX","auth":"tBHItJI5svbpez7KI4CCXg"}}`

func TestServePushKeyAnswersTheDaemonsVAPIDPublicKey(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodPushKey] = rpc.PushKey{PublicKey: "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/push/key", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "push-key.json", body)
}

func TestServePushSubscribeStoresTheSubscriptionWithTheCallingDevice(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.devices["tablet-token"] = tablet
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, token := range []string{goodToken, "tablet-token"} {
		status, body := do(t, ts, "POST", "/api/v1/push/subscribe", token, browserSubscription)
		if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("status %d %s", status, body)
		}
	}
	want := []string{
		`{"device":"k3m9p2qx","endpoint":"https://web.push.apple.com/QGx3","keys":{"p256dh":"BNcRdreALRFX","auth":"tBHItJI5svbpez7KI4CCXg"}}`,
		`{"device":"t4bl3t00","endpoint":"https://web.push.apple.com/QGx3","keys":{"p256dh":"BNcRdreALRFX","auth":"tBHItJI5svbpez7KI4CCXg"}}`,
	}
	got := f.paramsOf(rpc.MethodPushSubscribe)
	if len(got) != 2 || string(got[0]) != want[0] || string(got[1]) != want[1] {
		t.Fatalf("push.subscribe params %s", got)
	}
}

func TestServePushSubscribeRefusesABodyThatIsNoSubscription(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, body := range []string{`nope`, `{}`, `{"endpoint":"https://web.push.apple.com/QGx3"}`} {
		status, out := do(t, ts, "POST", "/api/v1/push/subscribe", goodToken, body)
		if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
			t.Fatalf("%s: status %d %s", body, status, out)
		}
	}
	if slices.Contains(f.methods(), rpc.MethodPushSubscribe) {
		t.Fatal("a bad body reached push.subscribe")
	}
	f.errs[rpc.MethodPushSubscribe] = &rpc.Error{Code: rpc.CodeBadRequest, Message: "the endpoint must be an https URL"}
	if status, out := do(t, ts, "POST", "/api/v1/push/subscribe", goodToken, browserSubscription); status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
		t.Fatalf("daemon refusal: status %d %s", status, out)
	}
}

func TestServePushUnsubscribeDropsTheCallingDevicesSubscription(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/push/unsubscribe", goodToken, "")
	if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("status %d %s", status, body)
	}
	if got := f.paramsOf(rpc.MethodPushUnsubscribe); len(got) != 1 || string(got[0]) != `{"device":"k3m9p2qx"}` {
		t.Fatalf("push.unsubscribe params %s", got)
	}
}
