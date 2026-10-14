package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/version"
)

func browserDaemon(t *testing.T) string {
	t.Helper()
	home := shortHome(t)
	t.Setenv("CREW_HOME", home)
	t.Setenv("CREW_SESSION", "")
	t.Setenv("TMUX_PANE", "")
	d, err := daemon.New(nopStore{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return home
}

func serveBrowser(t *testing.T, home string, answer func(rpc.BrowserCall) (any, string)) <-chan rpc.BrowserCall {
	t.Helper()
	conn, err := net.Dial("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	write := func(id uint64, method string, params any) {
		raw, _ := json.Marshal(params)
		line, _ := json.Marshal(rpc.Request{V: rpc.Version, ID: id, Method: method, Params: raw, Build: version.String()})
		_, _ = conn.Write(append(line, '\n'))
	}
	r := bufio.NewReader(conn)
	write(1, rpc.MethodBrowserAttach, struct{}{})
	if line, err := r.ReadBytes('\n'); err != nil || !strings.Contains(string(line), `"id":1`) {
		t.Fatalf("attach: %s %v", line, err)
	}
	calls := make(chan rpc.BrowserCall, 8)
	go func() {
		id := uint64(10)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var resp rpc.Response
			if json.Unmarshal(line, &resp) != nil || resp.Browser == nil {
				continue
			}
			calls <- *resp.Browser
			result, msg := answer(*resp.Browser)
			raw, _ := json.Marshal(result)
			id++
			write(id, rpc.MethodBrowserReply, rpc.BrowserReplyParams{Call: resp.Browser.Call, Result: raw, Error: msg})
		}
	}()
	return calls
}

func TestBrowserCLIWithoutTheMacAppFailsWithAClearError(t *testing.T) {
	browserDaemon(t)
	var stderr bytes.Buffer
	if code := run([]string{"browser", "--session", "s1", "snapshot"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), rpc.NoBrowser) {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestBrowserCLIRefusesBadArgumentsWithUsage(t *testing.T) {
	browserDaemon(t)
	var stderr bytes.Buffer
	if code := run([]string{"browser", "--session", "s1", "click", "e1"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "not an element ref") || !strings.Contains(stderr.String(), "usage: crew browser") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestBrowserCLISendsTheCommandForTheSessionAndPrintsTheSnapshotText(t *testing.T) {
	home := browserDaemon(t)
	t.Setenv("CREW_SESSION", "s1")
	calls := serveBrowser(t, home, func(rpc.BrowserCall) (any, string) {
		return map[string]string{"url": "http://devbox:3000/", "title": "api", "text": "- button \"Log in\" @e1\n"}, ""
	})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"browser", "snapshot"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	call := <-calls
	if call.Session != "s1" || call.Command != "snapshot" {
		t.Fatalf("call %+v", call)
	}
	if stdout.String() != "- button \"Log in\" @e1\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestBrowserCLIJSONPrintsTheWholeResult(t *testing.T) {
	home := browserDaemon(t)
	calls := serveBrowser(t, home, func(rpc.BrowserCall) (any, string) {
		return map[string]string{"url": "http://devbox:3000/login"}, ""
	})
	var stdout bytes.Buffer
	if code := run([]string{"browser", "--json", "--session", "s2", "goto", "3000/login"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	call := <-calls
	var args map[string]string
	_ = json.Unmarshal(call.Args, &args)
	if call.Session != "s2" || args["url"] != "3000/login" {
		t.Fatalf("call %+v %s", call, call.Args)
	}
	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got["url"] != "http://devbox:3000/login" {
		t.Fatalf("stdout %q: %v", stdout.String(), err)
	}
}

func TestBrowserCLIUsesTheAgentPaneWhenNoSessionIsGiven(t *testing.T) {
	home := browserDaemon(t)
	t.Setenv("TMUX_PANE", "%4")
	serveBrowser(t, home, func(rpc.BrowserCall) (any, string) { return "ok", "" })
	var stderr bytes.Buffer
	code := run([]string{"browser", "tabs"}, io.Discard, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "%4") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestBrowserCLIScreenshotWritesAPNGAndPrintsItsPath(t *testing.T) {
	home := browserDaemon(t)
	png := []byte("\x89PNG\r\n\x1a\nfake")
	serveBrowser(t, home, func(rpc.BrowserCall) (any, string) {
		return map[string]any{"png": base64.StdEncoding.EncodeToString(png), "width": 393, "height": 852}, ""
	})
	out := filepath.Join(t.TempDir(), "shot.png")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"browser", "--session", "s1", "--out", out, "screenshot"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != out {
		t.Fatalf("stdout %q", stdout.String())
	}
	if got, err := os.ReadFile(out); err != nil || !bytes.Equal(got, png) {
		t.Fatalf("file %q: %v", got, err)
	}
	stdout.Reset()
	if code := run([]string{"browser", "--session", "s1", "--json", "screenshot"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var got struct {
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		PNG    string `json:"png"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got.Width != 393 || got.PNG != "" || !strings.HasSuffix(got.Path, ".png") {
		t.Fatalf("stdout %q: %v", stdout.String(), err)
	}
	t.Cleanup(func() { _ = os.Remove(got.Path) })
	if data, err := os.ReadFile(got.Path); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("temp file: %v", err)
	}
}

func TestBrowserCLIPassesItsTimeout(t *testing.T) {
	home := browserDaemon(t)
	serveBrowser(t, home, func(rpc.BrowserCall) (any, string) {
		time.Sleep(400 * time.Millisecond)
		return "late", ""
	})
	var stderr bytes.Buffer
	code := run([]string{"browser", "--session", "s1", "--timeout", "100ms", "wait", "Done"}, io.Discard, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "no reply") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}
