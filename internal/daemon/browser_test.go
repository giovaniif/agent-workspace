package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/version"
)

type fakeBrowser struct {
	t    testing.TB
	conn net.Conn
	r    *bufio.Reader
	next uint64
}

func attachBrowser(t testing.TB, path string) *fakeBrowser {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	b := &fakeBrowser{t: t, conn: conn, r: bufio.NewReader(conn), next: 100}
	resp := b.request(rpc.MethodBrowserAttach, struct{}{})
	if resp.Error != nil {
		t.Fatalf("attach: %v", resp.Error)
	}
	return b
}

func (b *fakeBrowser) send(method string, params any) uint64 {
	b.t.Helper()
	b.next++
	raw, err := json.Marshal(params)
	if err != nil {
		b.t.Fatal(err)
	}
	line, err := json.Marshal(rpc.Request{V: rpc.Version, ID: b.next, Method: method, Params: raw, Build: version.String()})
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.conn.Write(append(line, '\n')); err != nil {
		b.t.Fatal(err)
	}
	return b.next
}

func (b *fakeBrowser) read() rpc.Response {
	b.t.Helper()
	_ = b.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := b.r.ReadBytes('\n')
	if err != nil {
		b.t.Fatalf("read: %v", err)
	}
	var resp rpc.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		b.t.Fatalf("%s: %v", line, err)
	}
	return resp
}

func (b *fakeBrowser) request(method string, params any) rpc.Response {
	b.t.Helper()
	id := b.send(method, params)
	for {
		resp := b.read()
		if resp.ID == id && resp.Browser == nil {
			return resp
		}
	}
}

func (b *fakeBrowser) call() rpc.BrowserCall {
	b.t.Helper()
	for {
		resp := b.read()
		if resp.Browser != nil {
			return *resp.Browser
		}
	}
}

func (b *fakeBrowser) reply(call uint64, result any, msg string) {
	b.t.Helper()
	var raw json.RawMessage
	if result != nil {
		var err error
		if raw, err = json.Marshal(result); err != nil {
			b.t.Fatal(err)
		}
	}
	b.send(rpc.MethodBrowserReply, rpc.BrowserReplyParams{Call: call, Result: raw, Error: msg})
}

func browserCommand(c *rpc.Client, p rpc.BrowserCommandParams) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Call(context.Background(), rpc.MethodBrowserCommand, p, &out)
	return out, err
}

func rpcMessage(err error) string {
	if rerr, ok := err.(*rpc.Error); ok {
		return rerr.Message
	}
	return ""
}

func TestBrowserCommandWithoutTheMacAppFailsAtOnce(t *testing.T) {
	_, path := start(t, &memStore{})
	c := dial(t, path)
	began := time.Now()
	_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"})
	if errCode(err) != rpc.CodeUnavailable || rpcMessage(err) != rpc.NoBrowser {
		t.Fatalf("got %v", err)
	}
	if took := time.Since(began); took > 500*time.Millisecond {
		t.Fatalf("took %v", took)
	}
}

func TestBrowserCommandGoesToTheAttachedAppAndItsReplyComesBack(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	c := dial(t, path)
	type got struct {
		out json.RawMessage
		err error
	}
	done := make(chan got, 1)
	go func() {
		out, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "goto", Args: map[string]any{"url": "http://devbox:3000/"}})
		done <- got{out, err}
	}()
	call := app.call()
	if call.Session != "s1" || call.Command != "goto" || call.Call == 0 {
		t.Fatalf("call %+v", call)
	}
	var args map[string]string
	if err := json.Unmarshal(call.Args, &args); err != nil || args["url"] != "http://devbox:3000/" {
		t.Fatalf("args %s: %v", call.Args, err)
	}
	app.reply(call.Call, map[string]string{"url": "http://devbox:3000/", "title": "api"}, "")
	select {
	case g := <-done:
		if g.err != nil {
			t.Fatal(g.err)
		}
		var res map[string]string
		if err := json.Unmarshal(g.out, &res); err != nil || res["title"] != "api" {
			t.Fatalf("result %s: %v", g.out, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no result")
	}
}

func TestBrowserCommandFromAnAgentPaneUsesTheSessionOnThatPane(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s7", Harness: domain.HarnessClaude, Pane: "%7", State: domain.StateRunning}})
	next(t, sub.Diffs)
	app := attachBrowser(t, path)
	other := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(other, rpc.BrowserCommandParams{Pane: "%7", Command: "snapshot"})
		done <- err
	}()
	call := app.call()
	if call.Session != "s7" {
		t.Fatalf("session %q", call.Session)
	}
	app.reply(call.Call, map[string]string{"tree": "- button @e1"}, "")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, err = browserCommand(other, rpc.BrowserCommandParams{Pane: "%99", Command: "snapshot"})
	if errCode(err) != rpc.CodeBadRequest || !strings.Contains(rpcMessage(err), "session") {
		t.Fatalf("unknown pane: %v", err)
	}
	_, err = browserCommand(other, rpc.BrowserCommandParams{Command: "snapshot"})
	if errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("no session: %v", err)
	}
}

func TestBrowserCommandNeedsACommand(t *testing.T) {
	_, path := start(t, &memStore{})
	attachBrowser(t, path)
	_, err := browserCommand(dial(t, path), rpc.BrowserCommandParams{Session: "s1"})
	if errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("got %v", err)
	}
}

func TestBrowserErrorFromTheAppFailsTheCommandWithItsMessage(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	c := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "click", Args: map[string]any{"ref": "@e9"}})
		done <- err
	}()
	call := app.call()
	app.reply(call.Call, nil, "no element @e9: take a new snapshot")
	err := <-done
	if errCode(err) != rpc.CodeFailed || rpcMessage(err) != "no element @e9: take a new snapshot" {
		t.Fatalf("got %v", err)
	}
}

func TestBrowserCommandWithNoReplyTimesOut(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	c := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "wait", Args: map[string]any{"text": "x"}, TimeoutMS: 100})
		done <- err
	}()
	call := app.call()
	select {
	case err := <-done:
		if errCode(err) != rpc.CodeFailed || !strings.Contains(rpcMessage(err), "no reply") {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no timeout")
	}
	resp := app.request(rpc.MethodBrowserReply, rpc.BrowserReplyParams{Call: call.Call})
	if resp.Error == nil || resp.Error.Code != rpc.CodeNotFound {
		t.Fatalf("late reply: %+v", resp.Error)
	}
}

func TestBrowserAppThatDisconnectsFailsTheCommandInFlight(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	c := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"})
		done <- err
	}()
	app.call()
	_ = app.conn.Close()
	select {
	case err := <-done:
		if errCode(err) != rpc.CodeUnavailable {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the command still waits")
	}
	_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"})
	if rpcMessage(err) != rpc.NoBrowser {
		t.Fatalf("after disconnect: %v", err)
	}
}

func TestANewBrowserAttachReplacesTheOldOne(t *testing.T) {
	_, path := start(t, &memStore{})
	old := attachBrowser(t, path)
	app := attachBrowser(t, path)
	c := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "tabs"})
		done <- err
	}()
	call := app.call()
	app.reply(call.Call, []string{}, "")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	resp := old.request(rpc.MethodBrowserReply, rpc.BrowserReplyParams{Call: call.Call})
	if resp.Error == nil || resp.Error.Code != rpc.CodeNotFound {
		t.Fatalf("old attach reply: %+v", resp.Error)
	}
}

func TestBrowserReplyFromAnotherConnectionIsRefused(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	c := dial(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "tabs"})
		done <- err
	}()
	call := app.call()
	err := dial(t, path).Call(context.Background(), rpc.MethodBrowserReply, rpc.BrowserReplyParams{Call: call.Call, Result: json.RawMessage(`"forged"`)}, nil)
	if errCode(err) != rpc.CodeNotFound {
		t.Fatalf("forged reply: %v", err)
	}
	app.reply(call.Call, []string{"t1"}, "")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBrowserCommandsFromManySessionsKeepTheirOwnReplies(t *testing.T) {
	_, path := start(t, &memStore{})
	app := attachBrowser(t, path)
	sessions := []string{"a", "b", "c"}
	results := make(chan string, len(sessions))
	for _, s := range sessions {
		c := dial(t, path)
		go func(s string) {
			out, err := browserCommand(c, rpc.BrowserCommandParams{Session: s, Command: "tabs"})
			if err != nil {
				results <- "error " + err.Error()
				return
			}
			var got string
			_ = json.Unmarshal(out, &got)
			results <- s + "=" + got
		}(s)
	}
	for range sessions {
		call := app.call()
		app.reply(call.Call, call.Session, "")
	}
	var got []string
	for range sessions {
		got = append(got, <-results)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "a=a,b=b,c=c" {
		t.Fatalf("got %v", got)
	}
}

func BenchmarkBrowserRoundTrip(b *testing.B) {
	_, path := start(b, &memStore{})
	app := attachBrowser(b, path)
	go func() {
		for {
			_ = app.conn.SetReadDeadline(time.Now().Add(time.Minute))
			line, err := app.r.ReadBytes('\n')
			if err != nil {
				return
			}
			var resp rpc.Response
			if json.Unmarshal(line, &resp) != nil || resp.Browser == nil {
				continue
			}
			raw, _ := json.Marshal(rpc.BrowserReplyParams{Call: resp.Browser.Call, Result: json.RawMessage(`{}`)})
			req, _ := json.Marshal(rpc.Request{V: rpc.Version, ID: resp.Browser.Call + 1_000_000, Method: rpc.MethodBrowserReply, Params: raw, Build: version.String()})
			if _, err := app.conn.Write(append(req, '\n')); err != nil {
				return
			}
		}
	}()
	c := dial(b, path)
	b.ResetTimer()
	began := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := browserCommand(c, rpc.BrowserCommandParams{Session: "s1", Command: "tabs"}); err != nil {
			b.Fatal(err)
		}
	}
	if perOp := time.Since(began) / time.Duration(b.N); perOp > 5*time.Millisecond {
		b.Fatalf("a browser round trip through the daemon took %v, budget 5ms", perOp)
	}
}
