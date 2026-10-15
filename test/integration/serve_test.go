//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
	gitadapter "github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

const serveOrigin = "https://box.example.ts.net"

func startServeDaemon(t *testing.T) (string, *rpc.Client) {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "crew-serve")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := daemon.New(store, os.Getpid(), daemon.WithWorkspaces(wsfs.FS{}, gitadapter.Inspector{}),
		daemon.WithTranscripts(daemon.Transcripts(wsfs.Transcripts{}), wsfs.Transcripts{}))
	if err != nil {
		t.Fatal(err)
	}
	path := rpc.SocketPath(home)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Serve(ctx, ln)
		_ = store.Close()
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return path, c
}

func startServe(t *testing.T, socket string) *httptest.Server {
	t.Helper()
	srv, err := serve.New(serve.Config{
		Build: "test",
		URL:   serveOrigin,
		Dial: func(context.Context) (serve.Daemon, error) {
			return rpc.Dial(socket)
		},
	})
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
	return ts
}

func request(t *testing.T, ts *httptest.Server, method, path, token, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func pairDevice(t *testing.T, c *rpc.Client, ts *httptest.Server) rpc.PairRedeemed {
	t.Helper()
	var code rpc.PairCode
	if err := c.Call(context.Background(), rpc.MethodPairCode, rpc.PairCodeParams{}, &code); err != nil {
		t.Fatal(err)
	}
	status, body := request(t, ts, "POST", "/api/v1/pair", "", `{"code":"`+code.Code+`","name":"phone"}`)
	if status != http.StatusOK {
		t.Fatalf("pair: %d %s", status, body)
	}
	var paired rpc.PairRedeemed
	if err := json.Unmarshal(body, &paired); err != nil || paired.Token == "" || paired.Device.Name != "phone" {
		t.Fatalf("pair body %s (%v)", body, err)
	}
	return paired
}

func TestServeAgainstARealDaemonPairsAnswersAndClosesARevokedStream(t *testing.T) {
	socket, c := startServeDaemon(t)
	ts := startServe(t, socket)

	if status, body := request(t, ts, "GET", "/api/v1/hello", "", ""); status != http.StatusOK || !strings.Contains(string(body), `"api":"v1"`) {
		t.Fatalf("hello: %d %s", status, body)
	}
	if status, _ := request(t, ts, "POST", "/api/v1/pair", "", `{"code":"WRONG234"}`); status != http.StatusUnauthorized {
		t.Fatalf("a wrong code paired: %d", status)
	}
	paired := pairDevice(t, c, ts)

	if status, _ := request(t, ts, "GET", "/api/v1/workspaces", "", ""); status != http.StatusUnauthorized {
		t.Fatalf("workspaces without a token: %d", status)
	}
	status, body := request(t, ts, "GET", "/api/v1/workspaces", paired.Token, "")
	if status != http.StatusOK || !strings.Contains(string(body), `"workspaces":[]`) {
		t.Fatalf("workspaces: %d %s", status, body)
	}
	if status, _ := request(t, ts, "POST", "/api/v1/sessions/nope/end", paired.Token, ""); status != http.StatusNotFound {
		t.Fatalf("ending an unknown session: %d", status)
	}
	if status, body := request(t, ts, "GET", "/api/v1/sessions/nope/messages?limit=10", paired.Token, ""); status != http.StatusNotFound || !strings.Contains(string(body), `"not_found"`) {
		t.Fatalf("messages of an unknown session: %d %s", status, body)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}}); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another origin: err %v resp %v", err, resp)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {serveOrigin}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	first, _ := json.Marshal(map[string]string{"token": paired.Token})
	if err := conn.Write(ctx, websocket.MessageText, first); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame serve.Frame
	if err := json.Unmarshal(data, &frame); err != nil || frame.State == nil || frame.State.Sessions == nil {
		t.Fatalf("first frame %s (%v)", data, err)
	}
	watch, _ := json.Marshal(map[string]any{"watch": "nope", "after": 0})
	if err := conn.Write(ctx, websocket.MessageText, watch); err != nil {
		t.Fatal(err)
	}
	if _, data, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	frame = serve.Frame{}
	if err := json.Unmarshal(data, &frame); err != nil || frame.Watch != "nope" || frame.Error == nil || frame.Error.Code != rpc.CodeNotFound {
		t.Fatalf("watching an unknown session: %s (%v)", data, err)
	}

	if err := c.Call(context.Background(), rpc.MethodDeviceRevoke, rpc.DeviceRevokeParams{ID: paired.Device.ID}, nil); err != nil {
		t.Fatal(err)
	}
	revoked := time.Now()
	readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
	defer cancelRead()
	for {
		_, data, err := conn.Read(readCtx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != serve.CloseUnauthorized {
			t.Fatalf("stream ended with %v after %v (last frame %s)", err, time.Since(revoked), data)
		}
		break
	}
	if status, _ := request(t, ts, "GET", "/api/v1/workspaces", paired.Token, ""); status != http.StatusUnauthorized {
		t.Fatalf("a revoked token still works: %d", status)
	}
}
