package serve_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/agent-workspace/internal/serve"
)

const secretToken = "s3cret-device-token"

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := strings.TrimSpace(b.buf.String())
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func (b *syncBuffer) waitFor(t *testing.T, want string) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		lines := b.lines()
		for _, l := range lines {
			if strings.Contains(l, want) {
				return lines
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no log line with %q in %q", want, lines)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func neverLogsTheToken(t *testing.T, lines []string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, secretToken) {
			t.Fatalf("the log has the token: %q", l)
		}
	}
}

func TestServeLogsAStreamClosedForALateToken(t *testing.T) {
	f := newFakeDaemon()
	var log syncBuffer
	_, ts := startServer(t, f, serve.Config{URL: publicURL, AuthTimeout: 50 * time.Millisecond, Log: &log})
	c, _, err := dialStream(t, ts, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	closedWith(t, c, 2*time.Second)
	lines := log.waitFor(t, "stream closed 4400")
	if !strings.Contains(strings.Join(lines, "\n"), "no token within") {
		t.Fatalf("the log does not say why: %q", lines)
	}
}

func TestServeLogsARefusedStreamTokenWithoutTheToken(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	var log syncBuffer
	_, ts := startServer(t, f, serve.Config{URL: publicURL, Log: &log})
	c := openStream(t, ts, secretToken)
	closedWith(t, c, 2*time.Second)
	neverLogsTheToken(t, log.waitFor(t, "stream closed 4401"))
}

func TestServeLogsUnauthorizedRequestsWithoutTheTokenOrTheQuery(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	var log syncBuffer
	_, ts := startServer(t, f, serve.Config{URL: publicURL, Log: &log})
	do(t, ts, "GET", "/api/v1/workspaces", secretToken, "")
	do(t, ts, "GET", "/api/v1/work-items/resolve?item="+secretToken, "", "")
	log.waitFor(t, "GET /api/v1/workspaces 401 unauthorized")
	neverLogsTheToken(t, log.waitFor(t, "GET /api/v1/work-items/resolve 401 unauthorized"))
}

func TestServeDoesNotLogAStreamTheClientClosesNormally(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	var log syncBuffer
	_, ts := startServer(t, f, serve.Config{URL: publicURL, Log: &log})
	c := openStream(t, ts, goodToken)
	next(t, c)
	if err := c.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for closesOf(f) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	do(t, ts, "GET", "/api/v1/workspaces", "", "")
	if lines := log.waitFor(t, "401"); len(lines) != 1 {
		t.Fatalf("logged more than the 401: %q", lines)
	}
}

func TestServeRateLimitsItsLogAndSaysHowManyLinesItDropped(t *testing.T) {
	f := newFakeDaemon()
	var log syncBuffer
	clk := &clock{now: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)}
	_, ts := startServer(t, f, serve.Config{URL: publicURL, Log: &log, Now: clk.Now})
	for range serve.LogBurst + 5 {
		do(t, ts, "GET", "/api/v1/workspaces", "", "")
	}
	if got := len(log.lines()); got != serve.LogBurst {
		t.Fatalf("logged %d lines, want %d", got, serve.LogBurst)
	}
	clk.add(serve.LogWindow)
	do(t, ts, "GET", "/api/v1/push/key", "", "")
	lines := log.lines()
	if len(lines) != serve.LogBurst+2 || !strings.Contains(lines[serve.LogBurst], "dropped 5 log lines") {
		t.Fatalf("after the window: %q", lines[serve.LogBurst:])
	}
}
