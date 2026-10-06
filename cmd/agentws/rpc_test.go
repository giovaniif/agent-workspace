package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestRpcWithNoDaemonPrintsUnavailableAndExitsNonZeroWithoutStartingOne(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	var stdout, stderr bytes.Buffer
	code := runRPC(nil, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit 0 with no daemon, stderr %q", stderr.String())
	}
	var resp rpc.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil || resp.Error == nil || resp.Error.Code != rpc.CodeUnavailable || resp.V != rpc.Version {
		t.Fatalf("stdout %q is not one unavailable error line", stdout.String())
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout %q is not exactly one line", stdout.String())
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(rpc.SocketPath(home)); err == nil {
		t.Fatal("agentws rpc started a daemon")
	}
}

func fakeSocket(t *testing.T, home string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestRpcCopiesLinesBothWaysAndExitsZeroWhenTheSocketCloses(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	ln := fakeSocket(t, home)
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		line, _ := bufio.NewReader(c).ReadString('\n')
		got <- line
		_, _ = io.WriteString(c, "{\"v\":1,\"id\":1,\"result\":{}}\n{\"v\":1,\"id\":1,\"diff\":{}}\n")
		_ = c.Close()
	}()
	inR, inW := io.Pipe()
	defer func() { _ = inW.Close() }()
	go func() { _, _ = io.WriteString(inW, "{\"v\":1,\"id\":1,\"method\":\"subscribe\"}\n") }()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runRPC(nil, inR, &stdout, &stderr) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agentws rpc kept running after the socket closed")
	}
	if line := <-got; line != "{\"v\":1,\"id\":1,\"method\":\"subscribe\"}\n" {
		t.Fatalf("daemon got %q", line)
	}
	if stdout.String() != "{\"v\":1,\"id\":1,\"result\":{}}\n{\"v\":1,\"id\":1,\"diff\":{}}\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestRpcEndsTheConnectionWhenStdinCloses(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	ln := fakeSocket(t, home)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, c)
		_ = c.Close()
	}()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runRPC(nil, strings.NewReader("{\"v\":1,\"id\":1,\"method\":\"status\"}\n"), &stdout, &stderr)
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agentws rpc did not end after stdin closed")
	}
}

func TestRpcRefusesALineOverTheMessageCap(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	ln := fakeSocket(t, home)
	received := make(chan int64, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		n, _ := io.Copy(io.Discard, c)
		received <- n
		_ = c.Close()
	}()
	big := strings.Repeat("x", rpc.MaxMessage+1) + "\n"
	var stdout, stderr bytes.Buffer
	code := runRPC(nil, strings.NewReader(big), &stdout, &stderr)
	if code == 0 {
		t.Fatal("an oversized line exited 0")
	}
	if n := <-received; n > rpc.MaxMessage {
		t.Fatalf("forwarded %d bytes of an oversized line", n)
	}
}

func TestRpcTakesNoArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRPC([]string{"x"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestRpcAnswersViewSubscribeWithDerivedStateAndDiffs(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	ln := fakeSocket(t, home)
	got := make(chan string, 2)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(c)
		first, _ := r.ReadString('\n')
		got <- first
		state := `{"seq":1,"tasks":[{"ID":"t1","Source":"text","Text":"add login"}],"worktrees":[{"ID":"w1","Repo":"api","Branch":"feat","Ports":[{"Port":3000}]}],"sessions":[{"ID":"s1","TaskID":"t1","State":"running","WorktreeIDs":["w1"]}]}`
		diff := `{"seq":2,"worktree":{"ID":"w1","Repo":"api","Branch":"feat","PR":{"Number":12,"Title":"Add login form","State":"OPEN"}}}`
		_, _ = io.WriteString(c, `{"v":1,"id":7,"result":`+state+"}\n"+`{"v":1,"id":7,"diff":`+diff+"}\n")
		second, _ := r.ReadString('\n')
		got <- second
		_, _ = io.WriteString(c, "{\"v\":1,\"id\":8,\"result\":{\"ok\":true}}\n")
		_ = c.Close()
	}()
	inR, inW := io.Pipe()
	defer func() { _ = inW.Close() }()
	go func() {
		_, _ = io.WriteString(inW, "{\"v\":1,\"id\":7,\"method\":\"view.subscribe\",\"build\":\"b1\"}\n{\"v\":1,\"id\":8,\"method\":\"status\"}\n")
	}()
	var stdout, stderr bytes.Buffer
	if code := runRPC(nil, inR, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	var sub rpc.Request
	if err := json.Unmarshal([]byte(<-got), &sub); err != nil || sub.Method != rpc.MethodSubscribe || sub.ID != 7 || sub.Build != "b1" {
		t.Fatalf("daemon got %+v", sub)
	}
	if line := <-got; line != "{\"v\":1,\"id\":8,\"method\":\"status\"}\n" {
		t.Fatalf("status was not passed through: %q", line)
	}
	type frame struct {
		ID     uint64          `json:"id"`
		Result json.RawMessage `json:"result"`
		Diff   *struct {
			Session *struct {
				Name  string `json:"name"`
				Board []any  `json:"board"`
			} `json:"session"`
		} `json:"diff"`
	}
	var frames []frame
	for _, l := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var f frame
		if err := json.Unmarshal([]byte(l), &f); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		frames = append(frames, f)
	}
	if len(frames) != 4 || frames[3].ID != 8 || string(frames[3].Result) != `{"ok":true}` {
		t.Fatalf("stdout %q", stdout.String())
	}
	var state struct {
		Worktrees []struct{ Ports []any } `json:"worktrees"`
		Sessions  []struct {
			Name  string `json:"name"`
			Order int    `json:"order"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(frames[0].Result, &state); err != nil || len(state.Sessions) != 1 || state.Sessions[0].Name != "add login" || len(state.Worktrees) != 1 || len(state.Worktrees[0].Ports) != 1 {
		t.Fatalf("state %s", frames[0].Result)
	}
	renamed := frames[2].Diff
	if renamed == nil || renamed.Session == nil || renamed.Session.Name == "add login" || len(renamed.Session.Board) != 1 {
		t.Fatalf("no renamed session diff: %q", stdout.String())
	}
}
