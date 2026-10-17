package serve_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

func TestServeMateRoutesCallTheDaemon(t *testing.T) {
	for _, c := range []struct {
		verb, path, body, method, params, want string
	}{
		{"GET", "/api/v1/mates", "", rpc.MethodMateList, `{}`, `"mates":[]`},
		{"POST", "/api/v1/mates", `{"mate":{"name":"helper","prompt":"Help.","agent":"claude","enabled":true}}`, rpc.MethodMateSet, `{"mate":{"name":"helper","prompt":"Help.","agent":"claude","enabled":true}}`, ``},
		{"POST", "/api/v1/mates/stop", `{"stop":true}`, rpc.MethodMateStopAll, `{"stop":true}`, ``},
		{"POST", "/api/v1/mates/stop", `{"stop":false}`, rpc.MethodMateStopAll, `{"stop":false}`, ``},
		{"POST", "/api/v1/mates/helper/pause", "", rpc.MethodMatePause, `{"name":"helper"}`, ``},
		{"POST", "/api/v1/mates/helper/resume", "", rpc.MethodMateResume, `{"name":"helper"}`, ``},
		{"POST", "/api/v1/mates/helper/tokens", `{"bot_token":"xoxb-1","app_token":"xapp-1","name":"other"}`, rpc.MethodMateSetTokens, `{"name":"helper","bot_token":"xoxb-1","app_token":"xapp-1"}`, ``},
		{"POST", "/api/v1/mates/helper/claim", "", rpc.MethodMateClaim, `{"name":"helper"}`, ``},
	} {
		f := newFakeDaemon()
		f.devices[goodToken] = phone
		_, ts := startServer(t, f, serve.Config{URL: publicURL})
		status, out := do(t, ts, c.verb, c.path, goodToken, c.body)
		if status != http.StatusOK || !strings.Contains(string(out), c.want) {
			t.Fatalf("%s %s: status %d %s", c.verb, c.path, status, out)
		}
		params := f.paramsOf(c.method)
		if len(params) != 1 || string(params[0]) != c.params {
			t.Fatalf("%s %s: params %s", c.verb, c.path, params)
		}
	}
}

func TestServeMateSetupGivesTheManifestLink(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodMateSetup] = rpc.MateSetup{Manifest: "{}", URL: "https://api.slack.com/apps?new_app=1"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/mates/helper/setup", goodToken, "")
	var got serve.MateSetup
	if err := json.Unmarshal(out, &got); err != nil || status != http.StatusOK {
		t.Fatalf("status %d %s %v", status, out, err)
	}
	if got.Manifest != "{}" || got.URL != "https://api.slack.com/apps?new_app=1" {
		t.Fatalf("setup %+v", got)
	}
	if params := f.paramsOf(rpc.MethodMateSetup); len(params) != 1 || string(params[0]) != `{"name":"helper"}` {
		t.Fatalf("params %s", params)
	}
}

func TestServeMateRoutesNeedAToken(t *testing.T) {
	f := newFakeDaemon()
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	if status, _ := do(t, ts, "POST", "/api/v1/mates/stop", "", `{"stop":true}`); status != http.StatusUnauthorized {
		t.Fatalf("status %d", status)
	}
	if len(f.paramsOf(rpc.MethodMateStopAll)) != 0 {
		t.Fatal("the daemon got the call")
	}
}
