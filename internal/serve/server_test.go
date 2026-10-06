package serve_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const (
	publicURL = "https://agentws.example.ts.net"
	goodToken = "good-token"
)

var (
	paired  = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	phone   = rpc.Device{ID: "k3m9p2qx", Name: "phone", CreatedAt: paired, LastSeen: paired}
	tablet  = rpc.Device{ID: "t4bl3t00", Name: "tablet", CreatedAt: paired, LastSeen: paired}
	allowed = []string{
		rpc.MethodDeviceCheck, rpc.MethodPairRedeem, rpc.MethodSubscribe, rpc.MethodWorkspaceList,
		rpc.MethodEndSession, rpc.MethodResumeSession, rpc.MethodSessionMute, rpc.MethodSessionRename,
		rpc.MethodTranscriptPage, rpc.MethodTranscriptWatch, rpc.MethodNewSession, rpc.MethodSessionResolve,
		rpc.MethodPushKey, rpc.MethodPushSubscribe, rpc.MethodPushUnsubscribe,
		rpc.MethodSessionSend, rpc.MethodSessionUnsend, rpc.MethodSessionInterrupt,
		rpc.MethodSessionPrompt, rpc.MethodSessionAnswer, rpc.MethodDeviceViewing,
	}
)

type authedEndpoint struct {
	method, path, body string
}

var authedEndpoints = []authedEndpoint{
	{"GET", "/api/v1/workspaces", ""},
	{"GET", "/api/v1/sessions/s1/messages", ""},
	{"POST", "/api/v1/sessions/s1/end", ""},
	{"POST", "/api/v1/sessions/s1/resume", ""},
	{"POST", "/api/v1/sessions/s1/mute", `{"muted":true}`},
	{"POST", "/api/v1/sessions/s1/rename", `{"name":"api"}`},
	{"POST", "/api/v1/sessions/s1/messages", `{"text":"run the tests"}`},
	{"DELETE", "/api/v1/sessions/s1/sends/q1", ""},
	{"POST", "/api/v1/sessions/s1/interrupt", ""},
	{"GET", "/api/v1/sessions/s1/prompt", ""},
	{"POST", "/api/v1/sessions/s1/answer", `{"choice":"1"}`},
	{"POST", "/api/v1/sessions", `{"work_item":"x","harness":"claude"}`},
	{"GET", "/api/v1/work-items/resolve?item=x", ""},
	{"GET", "/api/v1/push/key", ""},
	{"POST", "/api/v1/push/subscribe", browserSubscription},
	{"POST", "/api/v1/push/unsubscribe", ""},
}

func startServer(t *testing.T, f *fakeDaemon, cfg serve.Config) (*serve.Server, *httptest.Server) {
	t.Helper()
	cfg.Dial = f.dial
	if cfg.Build == "" {
		cfg.Build = "v0.12.0+test"
	}
	srv, err := serve.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler(nil))
	t.Cleanup(ts.Close)
	return srv, ts
}

func do(t *testing.T, ts *httptest.Server, method, path, token, body string, header ...string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, bytes.TrimSpace(got), "", "  "); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", name, err, got)
	}
	pretty.WriteByte('\n')
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/serve -update)", err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Fatalf("%s differs from the golden file\ngot:\n%s\nwant:\n%s", name, pretty.Bytes(), want)
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var e serve.ErrorBody
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error body %s: %v", body, err)
	}
	return e.Error.Code
}

func TestServeHelloAnswersWithoutATokenAndWithoutTheDaemon(t *testing.T) {
	f := newFakeDaemon()
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	before := len(f.methods())
	status, body := do(t, ts, "GET", "/api/v1/hello", "", "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "hello.json", body)
	if got := f.methods()[before:]; len(got) != 0 {
		t.Fatalf("hello called the daemon: %v", got)
	}
}

func TestServePairRedeemsTheCodeWithTheCallersAddress(t *testing.T) {
	f := newFakeDaemon()
	f.results[rpc.MethodPairRedeem] = rpc.PairRedeemed{Device: phone, Token: "dGhlLXRva2Vu"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/pair", "", `{"code":"ABCD2345","name":"phone"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "pair.json", body)
	params := f.paramsOf(rpc.MethodPairRedeem)
	var p rpc.PairRedeemParams
	if len(params) != 1 || json.Unmarshal(params[0], &p) != nil {
		t.Fatalf("pair.redeem calls %s", params)
	}
	if p.Code != "ABCD2345" || p.Name != "phone" || !strings.HasPrefix(p.Addr, "127.0.0.1:") {
		t.Fatalf("pair.redeem params %+v", p)
	}
}

func TestServePairTrustsTheForwardedAddressOnlyFromALocalProxy(t *testing.T) {
	cases := []struct {
		name, remote, forwarded, want string
	}{
		{"loopback proxy", "127.0.0.1:50000", "198.51.100.7", "198.51.100.7"},
		{"loopback proxy chain keeps the last hop", "127.0.0.1:50000", "10.9.9.9, 198.51.100.7", "198.51.100.7"},
		{"container proxy on a private network", "172.17.0.2:50000", "198.51.100.7", "198.51.100.7"},
		{"direct client over the tailnet", "100.101.102.103:50000", "198.51.100.7", "100.101.102.103:50000"},
		{"loopback proxy with a garbage header", "127.0.0.1:50000", "not-an-ip", "127.0.0.1:50000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDaemon()
			f.results[rpc.MethodPairRedeem] = rpc.PairRedeemed{Device: phone, Token: "x"}
			srv, err := serve.New(serve.Config{Dial: f.dial})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/v1/pair", strings.NewReader(`{"code":"ABCD2345"}`))
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			rec := httptest.NewRecorder()
			srv.Handler(nil).ServeHTTP(rec, req)
			var p rpc.PairRedeemParams
			params := f.paramsOf(rpc.MethodPairRedeem)
			if len(params) != 1 || json.Unmarshal(params[0], &p) != nil {
				t.Fatalf("pair.redeem calls %s (status %d)", params, rec.Code)
			}
			if p.Addr != tc.want {
				t.Fatalf("addr %q, want %q", p.Addr, tc.want)
			}
		})
	}
}

func TestServePairMapsDaemonErrorsToHTTPStatuses(t *testing.T) {
	cases := []struct {
		code   string
		status int
	}{
		{rpc.CodeUnauthorized, http.StatusUnauthorized},
		{rpc.CodeRateLimited, http.StatusTooManyRequests},
		{rpc.CodeBadRequest, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			f := newFakeDaemon()
			f.errs[rpc.MethodPairRedeem] = &rpc.Error{Code: tc.code, Message: "the pairing code is wrong or has expired"}
			_, ts := startServer(t, f, serve.Config{URL: publicURL})
			status, body := do(t, ts, "POST", "/api/v1/pair", "", `{"code":"WRONG234"}`)
			if status != tc.status || errorCode(t, body) != tc.code {
				t.Fatalf("status %d body %s", status, body)
			}
			if tc.code == rpc.CodeUnauthorized {
				golden(t, "error.json", body)
			}
		})
	}
}

func TestServePairRefusesABodyThatIsNotJSON(t *testing.T) {
	f := newFakeDaemon()
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/pair", "", `code=ABCD2345`)
	if status != http.StatusBadRequest || errorCode(t, body) != rpc.CodeBadRequest {
		t.Fatalf("status %d body %s", status, body)
	}
	if slices.Contains(f.methods(), rpc.MethodPairRedeem) {
		t.Fatal("pair.redeem called for a bad body")
	}
}

func TestServeEveryEndpointButHelloAndPairNeedsAValidToken(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, ep := range authedEndpoints {
		for _, token := range []string{"", "wrong-token"} {
			status, body := do(t, ts, ep.method, ep.path, token, ep.body)
			if status != http.StatusUnauthorized || errorCode(t, body) != rpc.CodeUnauthorized {
				t.Fatalf("%s %s with token %q: status %d %s", ep.method, ep.path, token, status, body)
			}
		}
		status, body := do(t, ts, ep.method, ep.path, "", ep.body, "Authorization", "Basic "+goodToken)
		if status != http.StatusUnauthorized {
			t.Fatalf("%s %s with a Basic header: status %d %s", ep.method, ep.path, status, body)
		}
	}
	for _, m := range f.methods() {
		if m != rpc.MethodDeviceCheck && m != rpc.MethodSubscribe {
			t.Fatalf("an unauthenticated request reached %s", m)
		}
	}
}

func TestServeReachesOnlyAllowlistedDaemonMethods(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, ep := range authedEndpoints {
		if status, body := do(t, ts, ep.method, ep.path, goodToken, ep.body); status != http.StatusOK {
			t.Fatalf("%s %s: status %d %s", ep.method, ep.path, status, body)
		}
	}
	forbidden := []authedEndpoint{
		{"POST", "/api/v1/shell/toggle", `{}`},
		{"POST", "/api/v1/nvim/open", `{}`},
		{"POST", "/api/v1/ports/kill", `{"pgids":[1]}`},
		{"GET", "/api/v1/disk", ""},
		{"POST", "/api/v1/cleanup", ""},
		{"POST", "/api/v1/cleanup/worktree", `{}`},
		{"GET", "/api/v1/review", ""},
		{"POST", "/api/v1/review/send", `{}`},
		{"POST", "/api/v1/rpc", `{"method":"ports.kill"}`},
		{"POST", "/api/v1/devices/k3m9p2qx/revoke", ""},
		{"GET", "/api/v1/sessions/s1/../../disk", ""},
		{"DELETE", "/api/v1/workspaces", ""},
	}
	for _, ep := range forbidden {
		status, body := do(t, ts, ep.method, ep.path, goodToken, ep.body)
		if status != http.StatusNotFound {
			t.Fatalf("%s %s: status %d %s", ep.method, ep.path, status, body)
		}
	}
	for _, m := range f.methods() {
		if !slices.Contains(allowed, m) {
			t.Fatalf("serve reached %s, which is not on the allowlist", m)
		}
	}
}

func TestServeWorkspacesListsTheDaemonsWorkspaces(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodWorkspaceList] = rpc.WorkspaceList{
		Workspaces: []domain.Workspace{{Root: "/home/me/api", Kind: domain.WorkspaceSingle, LastUsed: paired,
			Repos: []domain.Repo{{Name: "api", Path: "/home/me/api", DefaultBranch: "main", Branch: "main"}}}},
		LastUsed: "/home/me/api",
	}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/workspaces", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "workspaces.json", body)
}

func TestServeSessionActionsCallTheirDaemonMethod(t *testing.T) {
	cases := []struct {
		path, body, method, params string
	}{
		{"/api/v1/sessions/s1/end", "", rpc.MethodEndSession, `{"id":"s1"}`},
		{"/api/v1/sessions/s1/resume", "", rpc.MethodResumeSession, `{"id":"s1"}`},
		{"/api/v1/sessions/s1/mute", `{"muted":false}`, rpc.MethodSessionMute, `{"id":"s1","muted":false}`},
		{"/api/v1/sessions/s1/rename", `{"name":"login fix"}`, rpc.MethodSessionRename, `{"id":"s1","name":"login fix"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			f := newFakeDaemon()
			f.devices[goodToken] = phone
			_, ts := startServer(t, f, serve.Config{URL: publicURL})
			status, body := do(t, ts, "POST", tc.path, goodToken, tc.body)
			if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
				t.Fatalf("status %d %s", status, body)
			}
			params := f.paramsOf(tc.method)
			if len(params) != 1 || string(params[0]) != tc.params {
				t.Fatalf("%s params %s, want %s", tc.method, params, tc.params)
			}
		})
	}
}

func TestServeSessionEndAnswersWithTheEndedSession(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodEndSession] = domain.Session{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, State: domain.StateIdle, Ended: true}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/end", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "session-end.json", body)
}

func TestServeSessionActionsRefuseBadBodies(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, ep := range []authedEndpoint{
		{"POST", "/api/v1/sessions/s1/mute", `{}`},
		{"POST", "/api/v1/sessions/s1/mute", `nope`},
		{"POST", "/api/v1/sessions/s1/rename", `[1]`},
		{"POST", "/api/v1/sessions/s1/rename", `{"name":"` + strings.Repeat("x", 70<<10) + `"}`},
	} {
		status, body := do(t, ts, ep.method, ep.path, goodToken, ep.body)
		if status != http.StatusBadRequest || errorCode(t, body) != rpc.CodeBadRequest {
			t.Fatalf("%s %.40s: status %d %s", ep.path, ep.body, status, body)
		}
	}
	for _, m := range f.methods() {
		if m == rpc.MethodSessionMute || m == rpc.MethodSessionRename {
			t.Fatalf("a bad body reached %s", m)
		}
	}
}

func TestServePassesDaemonErrorsThrough(t *testing.T) {
	cases := []struct {
		code   string
		status int
	}{
		{rpc.CodeNotFound, http.StatusNotFound},
		{rpc.CodeStale, http.StatusConflict},
		{rpc.CodeFailed, http.StatusInternalServerError},
		{rpc.CodeLaunchFailed, http.StatusInternalServerError},
		{rpc.CodeUnavailable, http.StatusServiceUnavailable},
		{rpc.CodeVersionMismatch, http.StatusBadGateway},
		{rpc.CodeUnknownMethod, http.StatusNotImplemented},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			f := newFakeDaemon()
			f.devices[goodToken] = phone
			f.errs[rpc.MethodResumeSession] = &rpc.Error{Code: tc.code, Message: "no session s1"}
			_, ts := startServer(t, f, serve.Config{URL: publicURL})
			status, body := do(t, ts, "POST", "/api/v1/sessions/s1/resume", goodToken, "")
			if status != tc.status || errorCode(t, body) != tc.code {
				t.Fatalf("status %d %s", status, body)
			}
		})
	}
}

func TestServeAnswersUnavailableWhenTheDaemonIsDown(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	f.mu.Lock()
	f.down = true
	f.mu.Unlock()
	for _, ep := range []authedEndpoint{{"GET", "/api/v1/workspaces", ""}, {"POST", "/api/v1/pair", `{"code":"ABCD2345"}`}} {
		status, body := do(t, ts, ep.method, ep.path, goodToken, ep.body)
		if status != http.StatusServiceUnavailable || errorCode(t, body) != rpc.CodeUnavailable {
			t.Fatalf("%s: status %d %s", ep.path, status, body)
		}
	}
}

func TestServeRefusesARevokedDeviceWhoseCheckRacedTheRevocation(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	f.broadcast(rpc.Diff{Seq: 9, RevokedDevice: phone.ID})
	deadline := time.Now().Add(time.Second)
	for {
		status, body := do(t, ts, "GET", "/api/v1/workspaces", goodToken, "")
		if status == http.StatusUnauthorized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still answering a revoked device: %d %s", status, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeConnectsToTheDaemonBeforeAcceptingRequests(t *testing.T) {
	f := newFakeDaemon()
	startServer(t, f, serve.Config{URL: publicURL})
	if f.subscribers() != 1 {
		t.Fatalf("subscribers %d after Start, want 1", f.subscribers())
	}
	down := newFakeDaemon()
	down.down = true
	srv, err := serve.New(serve.Config{Dial: down.dial})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded without a daemon")
	}
}

func TestServeResubscribesAfterTheDaemonRestarts(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	f.mu.Lock()
	for c := range f.subs {
		for _, ch := range f.subs[c] {
			close(ch)
		}
		delete(f.subs, c)
	}
	f.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for f.subscribers() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("serve did not subscribe again")
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.broadcast(rpc.Diff{Seq: 3, RevokedDevice: phone.ID})
	deadline = time.Now().Add(time.Second)
	for {
		if status, _ := do(t, ts, "GET", "/api/v1/workspaces", goodToken, ""); status == http.StatusUnauthorized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a revocation after the restart was missed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeRefusesAPublicURLThatIsNotHTTP(t *testing.T) {
	for _, u := range []string{"ftp://example.com", "https://", "https://exa mple.com"} {
		if _, err := serve.New(serve.Config{URL: u}); err == nil {
			t.Fatalf("New accepted the public URL %q", u)
		}
	}
}

func TestServeTakesAPublicURLWithoutAScheme(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: "agentws.example.ts.net/"})
	c, _, err := dialStream(t, ts, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.CloseNow()
}

func TestServeLeavesNonAPIPathsToTheFallback(t *testing.T) {
	f := newFakeDaemon()
	srv, err := serve.New(serve.Config{Dial: f.dial})
	if err != nil {
		t.Fatal(err)
	}
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "app "+r.URL.Path)
	})
	h := srv.Handler(fallback)
	for path, want := range map[string]string{"/": "app /", "/sessions/s1": "app /sessions/s1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Body.String() != want {
			t.Fatalf("%s: %q", path, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v2/hello", nil))
	if rec.Code != http.StatusNotFound || errorCode(t, rec.Body.Bytes()) != rpc.CodeNotFound {
		t.Fatalf("unknown API path: %d %s", rec.Code, rec.Body.String())
	}
}

func TestServeMessagesPagesTheSessionsTranscript(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodTranscriptPage] = rpc.TranscriptPage{Before: 40, Messages: []rpc.Message{
		{ID: "u1", Cursor: 96, Turn: "p1", Role: "user", Text: "fix the login redirect", At: paired},
		{ID: "c1", Cursor: 210, Turn: "p1", Role: "tool", Text: "PASS", At: paired,
			Tool: &rpc.MessageTool{Name: "Bash", Summary: "go test ./...", Status: "done"}},
	}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/sessions/s1/messages?before=120&limit=25", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "messages.json", body)
	params := f.paramsOf(rpc.MethodTranscriptPage)
	if len(params) != 1 || string(params[0]) != `{"session":"s1","before":120,"limit":25}` {
		t.Fatalf("transcript.page params %s", params)
	}
	if status, body := do(t, ts, "GET", "/api/v1/sessions/s2/messages", goodToken, ""); status != http.StatusOK {
		t.Fatalf("newest page: %d %s", status, body)
	}
	params = f.paramsOf(rpc.MethodTranscriptPage)
	if len(params) != 2 || string(params[1]) != `{"session":"s2"}` {
		t.Fatalf("transcript.page params for the newest page %s", params)
	}
}

func TestServeMessagesRefusesAQueryThatIsNotANumber(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, q := range []string{"?before=abc", "?limit=ten", "?before=1.5"} {
		status, body := do(t, ts, "GET", "/api/v1/sessions/s1/messages"+q, goodToken, "")
		if status != http.StatusBadRequest || errorCode(t, body) != rpc.CodeBadRequest {
			t.Fatalf("%s: status %d %s", q, status, body)
		}
	}
	if slices.Contains(f.methods(), rpc.MethodTranscriptPage) {
		t.Fatal("a bad query reached transcript.page")
	}
}

func TestServeSendMessageAnswersWithTheSendIDAndWhetherItQueued(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodSessionSend] = rpc.SessionSent{ID: "q7", Queued: true}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/messages", goodToken, `{"text":"run the tests"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "send.json", body)
	params := f.paramsOf(rpc.MethodSessionSend)
	if len(params) != 1 || string(params[0]) != `{"session":"s1","text":"run the tests"}` {
		t.Fatalf("session.send params %s", params)
	}
}

func TestServeSendMessageRefusesABodyWithoutText(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, body := range []string{`{}`, `{"text":"   "}`, `nope`} {
		status, out := do(t, ts, "POST", "/api/v1/sessions/s1/messages", goodToken, body)
		if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
			t.Fatalf("%s: status %d %s", body, status, out)
		}
	}
	if slices.Contains(f.methods(), rpc.MethodSessionSend) {
		t.Fatal("a bad body reached session.send")
	}
}

func TestServeSendMessageUnsendDropsAQueuedSend(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "DELETE", "/api/v1/sessions/s1/sends/q7", goodToken, "")
	if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("status %d %s", status, body)
	}
	params := f.paramsOf(rpc.MethodSessionUnsend)
	if len(params) != 1 || string(params[0]) != `{"session":"s1","id":"q7"}` {
		t.Fatalf("session.unsend params %s", params)
	}
}

func TestServeSendMessageUnsendOfASendAlreadyPastedIsNotFound(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodSessionUnsend] = &rpc.Error{Code: rpc.CodeNotFound, Message: "no queued send q7"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "DELETE", "/api/v1/sessions/s1/sends/q7", goodToken, "")
	if status != http.StatusNotFound || errorCode(t, body) != rpc.CodeNotFound || !strings.Contains(string(body), "no queued send q7") {
		t.Fatalf("status %d %s", status, body)
	}
}

func TestServeInterruptCallsSessionInterrupt(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/interrupt", goodToken, "")
	if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("status %d %s", status, body)
	}
	params := f.paramsOf(rpc.MethodSessionInterrupt)
	if len(params) != 1 || string(params[0]) != `{"session":"s1"}` {
		t.Fatalf("session.interrupt params %s", params)
	}
}
