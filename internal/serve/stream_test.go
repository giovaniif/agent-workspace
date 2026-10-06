package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

func wsURL(ts *httptest.Server) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
}

func dialStream(t *testing.T, ts *httptest.Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	c, resp, err := websocket.Dial(ctx, wsURL(ts), &websocket.DialOptions{HTTPHeader: h})
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, resp, err
}

func openStream(t *testing.T, ts *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	c, _, err := dialStream(t, ts, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, map[string]string{"token": token})
	return c
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func next(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

func closedWith(t *testing.T, c *websocket.Conn, within time.Duration) (websocket.StatusCode, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			t.Fatalf("still open after %v (last frame %s)", within, data)
		}
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			return ce.Code, ce.Reason
		}
		return -1, err.Error()
	}
}

func streamState() rpc.State {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	return rpc.State{
		Seq:        41,
		Workspaces: []domain.Workspace{{Root: "/home/me/api", Kind: domain.WorkspaceSingle, LastUsed: at}},
		Tasks: []domain.Task{{ID: "t1", Source: domain.TaskText, Text: "fix the login redirect"},
			{ID: "t2", Source: domain.TaskText, Text: "add retries to the client"}},
		Worktrees: []domain.Worktree{{ID: "w1", Repo: "api", Path: "/home/me/.agentws/worktrees/api-login", Branch: "login",
			SessionID: "s1", Ports: []domain.Port{{Port: 5173, PID: 4242}}}},
		Sessions: []domain.Session{{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, Model: "opus", State: domain.StateRunning,
			WorktreeIDs: []string{"w1"}, Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 42, ResetsAt: at.Add(3 * time.Hour).Unix()},
				{Window: "seven_day", UsedPercent: 85}}, LimitsAt: at},
			{ID: "s2", TaskID: "t2", Harness: domain.HarnessCodex, State: domain.StateDone, Unread: true,
				Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 10}}, LimitsAt: at.Add(-time.Hour)}},
		Events: []domain.SessionEvent{{SessionID: "s1", Kind: domain.EventUserPromptSubmit, At: at},
			{SessionID: "s2", Kind: domain.EventUserPromptSubmit, At: at.Add(-10 * time.Minute)},
			{SessionID: "s2", Kind: domain.EventStop, Text: "Added the retry to the client.", At: at.Add(-5*time.Minute - 48*time.Second)}},
		Subagents: []domain.Subagent{{SessionID: "s1", ID: "a1"}},
		Queue:     []domain.LaunchItem{{ID: "q1", Ref: "#42", Workspace: "/home/me/api"}},
		Drafts:    []domain.ReviewDraft{{Session: "s1"}},
		Sends:     []domain.QueuedSend{{ID: "m1", Session: "s1", Text: "and add a test", QueuedAt: at}},
	}
}

func TestServeStreamRefusesAnotherOrigin(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, origin := range []string{"https://evil.example", "http://agentws.example.ts.net", "https://agentws.example.ts.net:8443", ""} {
		_, resp, err := dialStream(t, ts, origin)
		if err == nil {
			t.Fatalf("origin %q: the stream opened", origin)
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: response %v", origin, resp)
		}
	}
	for _, origin := range []string{"https://AgentWS.example.ts.net", "https://agentws.example.ts.net:443"} {
		c, _, err := dialStream(t, ts, origin)
		if err != nil {
			t.Fatalf("origin %q refused: %v", origin, err)
		}
		_ = c.CloseNow()
	}
}

func TestServeStreamRefusesEveryOriginWithoutAPublicURL(t *testing.T) {
	f := newFakeDaemon()
	_, ts := startServer(t, f, serve.Config{})
	_, resp, err := dialStream(t, ts, publicURL)
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("err %v resp %v", err, resp)
	}
}

func TestServeStreamClosesWithoutATokenInTime(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL, AuthTimeout: 100 * time.Millisecond})
	c, _, err := dialStream(t, ts, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	code, _ := closedWith(t, c, 2*time.Second)
	if code != serve.CloseBadHandshake {
		t.Fatalf("closed with %d, want the retryable %d", code, serve.CloseBadHandshake)
	}
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Fatalf("closed after %v, before the timeout", waited)
	}
}

func TestServeStreamClosesOnATokenTheDaemonRefusesAsUnauthorized(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, "wrong-token")
	if code, reason := closedWith(t, c, 2*time.Second); code != serve.CloseUnauthorized {
		t.Fatalf("closed with %d %q", code, reason)
	}
}

func TestServeStreamClosesOnAMalformedFirstFrameWithTheRetryableCode(t *testing.T) {
	for name, first := range map[string]string{
		"no token":      `{"watch":"s1"}`,
		"empty token":   `{"token":""}`,
		"not an object": `[1]`,
		"not JSON":      `hello`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeDaemon()
			f.devices[goodToken] = phone
			_, ts := startServer(t, f, serve.Config{URL: publicURL})
			c, _, err := dialStream(t, ts, publicURL)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := c.Write(ctx, websocket.MessageText, []byte(first)); err != nil {
				t.Fatal(err)
			}
			if code, reason := closedWith(t, c, 2*time.Second); code != serve.CloseBadHandshake {
				t.Fatalf("closed with %d %q, want %d", code, reason, serve.CloseBadHandshake)
			}
			if f.subscribers() != 1 {
				t.Fatalf("an unauthenticated stream subscribed (%d subscribers)", f.subscribers())
			}
		})
	}
}

func TestServeStreamSendsTheFilteredStateThenItsDiffs(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.state = streamState()
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	golden(t, "stream-state.json", next(t, c))

	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	session := domain.Session{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, State: domain.StatePermission, WorktreeIDs: []string{"w1"},
		Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 81, ResetsAt: at.Add(3 * time.Hour).Unix()}}, LimitsAt: at.Add(time.Minute)}
	sends := []domain.QueuedSend{}
	f.broadcast(rpc.Diff{Seq: 42, Event: &domain.SessionEvent{SessionID: "s1", Kind: domain.EventPermissionRequest, Tool: "Bash", Detail: "make test", At: at.Add(time.Minute)}, Session: &session})
	f.broadcast(rpc.Diff{Seq: 43, Subagent: &domain.Subagent{SessionID: "s1", ID: "a2"}})
	f.broadcast(rpc.Diff{Seq: 44, Draft: &domain.ReviewDraft{Session: "s1"}})
	f.broadcast(rpc.Diff{Seq: 45, Worktree: &domain.Worktree{ID: "w1", Repo: "api", Branch: "login-v2", Ports: []domain.Port{{Port: 8080}}}})
	f.broadcast(rpc.Diff{Seq: 46, Sends: &sends})
	f.broadcast(rpc.Diff{Seq: 47, RemovedSession: "s2"})
	f.broadcast(rpc.Diff{Seq: 48, Task: &domain.Task{ID: "t1", Source: domain.TaskText, Text: "fix the login redirect", PinnedName: "login fix"}})
	var frames []json.RawMessage
	for range 7 {
		frames = append(frames, next(t, c))
	}
	all, _ := json.Marshal(frames)
	golden(t, "stream-diffs.json", all)
}

func TestServeStreamClosesWhenItsDeviceIsRevoked(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.devices["tablet-token"] = tablet
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	phoneA := openStream(t, ts, goodToken)
	phoneB := openStream(t, ts, goodToken)
	other := openStream(t, ts, "tablet-token")
	for _, c := range []*websocket.Conn{phoneA, phoneB, other} {
		next(t, c)
	}
	f.broadcast(rpc.Diff{Seq: 50, RevokedDevice: phone.ID})
	for _, c := range []*websocket.Conn{phoneA, phoneB} {
		if code, reason := closedWith(t, c, time.Second); code != serve.CloseUnauthorized {
			t.Fatalf("closed with %d %q", code, reason)
		}
	}
	f.broadcast(rpc.Diff{Seq: 51, RemovedSession: "s9"})
	var frame serve.Frame
	if err := json.Unmarshal(next(t, other), &frame); err != nil || frame.Diff == nil || frame.Diff.RemovedSession != "s9" {
		t.Fatalf("the other device's stream got %+v (%v)", frame, err)
	}
	late := openStream(t, ts, goodToken)
	if code, _ := closedWith(t, late, time.Second); code != serve.CloseUnauthorized {
		t.Fatalf("a revoked device opened a stream (closed with %d)", code)
	}
}

func TestServeStreamClosesWhenTheDaemonGoesAway(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	f.mu.Lock()
	for conn := range f.subs {
		for _, ch := range f.subs[conn] {
			close(ch)
		}
		delete(f.subs, conn)
	}
	f.mu.Unlock()
	if code, reason := closedWith(t, c, time.Second); code != websocket.StatusTryAgainLater {
		t.Fatalf("closed with %d %q", code, reason)
	}
}

func TestServeStreamAnswersAnUnknownFrameWithAnError(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	send(t, c, map[string]string{"hello": "there"})
	var frame serve.Frame
	if err := json.Unmarshal(next(t, c), &frame); err != nil || frame.Error == nil || frame.Error.Code != rpc.CodeBadRequest {
		t.Fatalf("frame %+v (%v)", frame, err)
	}
}

func TestServeStreamReleasesItsDaemonConnectionWhenTheClientLeaves(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	if f.subscribers() != 2 {
		t.Fatalf("subscribers %d with one stream open", f.subscribers())
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for f.subscribers() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("subscribers %d after the client left", f.subscribers())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func dropSubscriptions(f *fakeDaemon) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for conn := range f.subs {
		for _, ch := range f.subs[conn] {
			close(ch)
		}
		delete(f.subs, conn)
	}
}

func TestServeStreamClosesOnARevocationWhileTheRevocationWatcherReconnects(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	dropSubscriptions(f)
	c := openStream(t, ts, goodToken)
	next(t, c)
	f.broadcast(rpc.Diff{Seq: 60, RevokedDevice: phone.ID})
	if code, reason := closedWith(t, c, 500*time.Millisecond); code != serve.CloseUnauthorized {
		t.Fatalf("closed with %d %q", code, reason)
	}
}

func TestServeStreamSubscribesBeforeItChecksTheToken(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	before := len(f.methods())
	c := openStream(t, ts, goodToken)
	next(t, c)
	got := f.methods()[before:]
	if len(got) < 2 || got[0] != rpc.MethodSubscribe || got[1] != rpc.MethodDeviceCheck {
		t.Fatalf("stream calls %v, want subscribe then device.check, so a revoke between them still reaches the stream", got)
	}
}

func waitWatch(t *testing.T, f *fakeDaemon, session string, n int) *fakeWatch {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ws := f.watchesOf(session); len(ws) >= n {
			return ws[n-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no watch %d of %s", n, session)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func cancelled(w *fakeWatch, within time.Duration) bool {
	select {
	case <-w.ctx.Done():
		return true
	case <-time.After(within):
		return false
	}
}

func TestServeStreamWatchAddsASessionsMessages(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.initial["s1"] = []rpc.Message{{ID: "a1", Cursor: 80, Turn: "p1", Role: "assistant", Text: "Looking at the redirect.", At: paired}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	send(t, c, map[string]any{"watch": "s1", "after": 64})
	w := waitWatch(t, f, "s1", 1)
	if w.after != 64 {
		t.Fatalf("watch after %d", w.after)
	}
	w.events <- rpc.TranscriptEvent{Messages: []rpc.Message{{ID: "c1", Cursor: 120, Turn: "p1", Role: "tool", At: paired,
		Tool: &rpc.MessageTool{Name: "Bash", Summary: "go test ./...", Status: "running"}}}}
	w.events <- rpc.TranscriptEvent{Reset: true}
	w.events <- rpc.TranscriptEvent{Closed: true}
	var frames []json.RawMessage
	for range 4 {
		frames = append(frames, next(t, c))
	}
	all, _ := json.Marshal(frames)
	golden(t, "stream-transcript.json", all)
}

func TestServeStreamUnwatchAndCloseEndTheDaemonWatch(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	send(t, c, map[string]any{"watch": "s1", "after": 0})
	first := waitWatch(t, f, "s1", 1)
	next(t, c)
	send(t, c, map[string]any{"watch": "s1", "after": 300})
	second := waitWatch(t, f, "s1", 2)
	if !cancelled(first, time.Second) {
		t.Fatal("watching a session again kept the old watch")
	}
	next(t, c)
	send(t, c, map[string]any{"unwatch": "s1"})
	if !cancelled(second, time.Second) {
		t.Fatal("unwatch kept the daemon watch")
	}
	send(t, c, map[string]any{"watch": "s2", "after": 0})
	third := waitWatch(t, f, "s2", 1)
	next(t, c)
	_ = c.Close(websocket.StatusNormalClosure, "")
	if !cancelled(third, time.Second) {
		t.Fatal("closing the stream kept the daemon watch")
	}
}

func TestServeStreamReportsAFailedWatch(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodTranscriptWatch] = &rpc.Error{Code: rpc.CodeNotFound, Message: "no session s9"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	next(t, c)
	send(t, c, map[string]any{"watch": "s9", "after": 0})
	golden(t, "stream-watch-error.json", next(t, c))
	send(t, c, map[string]any{"watch": "", "after": 0})
	var frame serve.Frame
	if err := json.Unmarshal(next(t, c), &frame); err != nil || frame.Error == nil || frame.Error.Code != rpc.CodeBadRequest {
		t.Fatalf("frame %+v (%v)", frame, err)
	}
}

func closesOf(f *fakeDaemon) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

func TestServeStreamClosesOnARevocationWhileItsWritesAreStalledAndTheRevocationWatcherReconnects(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	dropSubscriptions(f)
	c := openStream(t, ts, goodToken)
	next(t, c)
	big := strings.Repeat("x", 2<<20)
	for i := range 12 {
		f.broadcast(rpc.Diff{Seq: uint64(70 + i), Task: &domain.Task{ID: "t1", Source: domain.TaskText, Text: big}})
	}
	before := closesOf(f)
	f.broadcast(rpc.Diff{Seq: 90, RevokedDevice: phone.ID})
	deadline := time.Now().Add(500 * time.Millisecond)
	for closesOf(f) == before {
		if time.Now().After(deadline) {
			t.Fatal("a stream whose client stopped reading kept its daemon connection after its device was revoked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeStreamGoesAwayWhenServeStopsBeforeTheToken(t *testing.T) {
	f := newFakeDaemon()
	srv, err := serve.New(serve.Config{URL: publicURL, Dial: f.dial, Build: "v0.12.0+test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler(nil))
	t.Cleanup(ts.Close)
	c, _, err := dialStream(t, ts, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if code, reason := closedWith(t, c, 2*time.Second); code != websocket.StatusGoingAway {
		t.Fatalf("closed with %d %q", code, reason)
	}
}

func TestServeStreamGivesAMutedSessionItsBannerLine(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	f.state = rpc.State{
		Sessions: []domain.Session{{ID: "s1", Harness: domain.HarnessClaude, State: domain.StateWaiting, Muted: true}},
		Events: []domain.SessionEvent{{SessionID: "s1", Kind: domain.EventUserPromptSubmit, At: at},
			{SessionID: "s1", Kind: domain.EventWaitingForInput, Text: "Pick a port", At: at.Add(time.Minute)}},
	}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	c := openStream(t, ts, goodToken)
	var frame serve.Frame
	if err := json.Unmarshal(next(t, c), &frame); err != nil {
		t.Fatal(err)
	}
	got := frame.State.Sessions[0]
	if got.Banner != "waiting: Pick a port" || got.Since == nil || !got.Since.Equal(at.Add(time.Minute)) {
		t.Fatalf("banner %q since %v", got.Banner, got.Since)
	}
}
