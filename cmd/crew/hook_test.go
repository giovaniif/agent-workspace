package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agentws-h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func hook(home, event, stdin string) (int, string) {
	var out bytes.Buffer
	code := runHook([]string{"--harness", "claude", "--event", event}, strings.NewReader(stdin), &out, home, "%3")
	return code, out.String()
}

func TestHookWithTheDaemonDownExitsZeroAndLogsOneLine(t *testing.T) {
	home := shortHome(t)
	start := time.Now()
	code, out := hook(home, "Stop", `{"session_id":"x"}`)
	if code != 0 || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if took := time.Since(start); took > 60*time.Millisecond {
		t.Fatalf("took %v", took)
	}
	log, err := os.ReadFile(filepath.Join(home, "hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(log), "\n"); n != 1 {
		t.Fatalf("log has %d lines: %q", n, log)
	}
}

func TestHookWithBadArgumentsStillExitsZeroAndLogs(t *testing.T) {
	home := shortHome(t)
	var out bytes.Buffer
	if code := runHook([]string{"--event"}, strings.NewReader(""), &out, home, ""); code != 0 || out.Len() != 0 {
		t.Fatalf("code %d, stdout %q", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "hook.log")); err != nil {
		t.Fatal(err)
	}
}

func TestHookEventReachesDaemonStateWithin150ms(t *testing.T) {
	home := shortHome(t)
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

	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}})
	<-sub.Diffs

	start := time.Now()
	if code, out := hook(home, "Stop", `{"session_id":"x"}`); code != 0 || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	select {
	case diff := <-sub.Diffs:
		if diff.Session == nil || diff.Session.State != domain.StateDone {
			t.Fatalf("diff %+v", diff)
		}
	case <-time.After(150*time.Millisecond - time.Since(start)):
		t.Fatal("no state change within 150ms")
	}
	if _, err := os.Stat(filepath.Join(home, "hook.log")); err == nil {
		t.Fatal("hook logged an error with the daemon up")
	}
}

func fakeDaemon(t *testing.T, home string, reply func(rpc.Request) *rpc.Response) {
	t.Helper()
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = nc.Close() }()
				sc := bufio.NewScanner(nc)
				if !sc.Scan() {
					return
				}
				var req rpc.Request
				_ = json.Unmarshal(sc.Bytes(), &req)
				if resp := reply(req); resp != nil {
					resp.Build = req.Build
					_ = json.NewEncoder(nc).Encode(resp)
				}
				time.Sleep(time.Second)
			}()
		}
	}()
}

func TestUserPromptSubmitPrintsTheDaemonsReply(t *testing.T) {
	home := shortHome(t)
	fakeDaemon(t, home, func(req rpc.Request) *rpc.Response {
		b, _ := json.Marshal(rpc.HookReply{Output: json.RawMessage(`{"additionalContext":"hi"}`)})
		return &rpc.Response{V: rpc.Version, ID: req.ID, Result: b}
	})
	code, out := hook(home, "UserPromptSubmit", `{"prompt":"p"}`)
	if code != 0 || out != `{"additionalContext":"hi"}`+"\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
}

func TestUserPromptSubmitFallsBackToNoOutputAfter50ms(t *testing.T) {
	home := shortHome(t)
	fakeDaemon(t, home, func(rpc.Request) *rpc.Response { return nil })
	start := time.Now()
	code, out := hook(home, "UserPromptSubmit", `{"prompt":"p"}`)
	if took := time.Since(start); took < hookTimeout || took > 500*time.Millisecond {
		t.Fatalf("took %v", took)
	}
	if code != 0 || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	log, err := os.ReadFile(filepath.Join(home, "hook.log"))
	if err != nil || !strings.Contains(string(log), "no reply within 50ms") {
		t.Fatalf("hook.log %q, %v", log, err)
	}
}

func TestHookSendsPaneTimeAndPayload(t *testing.T) {
	home := shortHome(t)
	got := make(chan rpc.Request, 1)
	fakeDaemon(t, home, func(req rpc.Request) *rpc.Response { got <- req; return nil })
	before := time.Now()
	if code, out := hook(home, "PreToolUse", `{"tool_name":"Bash"}`); code != 0 || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	req := <-got
	var h rpc.Hook
	if err := json.Unmarshal(req.Params, &h); err != nil {
		t.Fatal(err)
	}
	if req.Method != rpc.MethodHook || h.Harness != "claude" || h.Event != "PreToolUse" || h.Pane != "%3" ||
		string(h.Payload) != `{"tool_name":"Bash"}` || h.At.Before(before.Add(-time.Second)) {
		t.Fatalf("request %+v, hook %+v", req, h)
	}
}
