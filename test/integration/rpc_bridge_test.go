//go:build integration

package integration

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/rpcpipe"
	"github.com/giovaniif/agent-workspace/internal/version"
)

type bridged struct {
	in    *io.PipeWriter
	lines *bufio.Scanner
	done  chan error
}

func startBridge(t *testing.T, socket string) bridged {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := rpcpipe.Run(socket, inR, outW)
		_ = outW.Close()
		done <- err
	}()
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 64*1024), rpc.MaxMessage)
	return bridged{in: inW, lines: sc, done: done}
}

func (b bridged) send(t *testing.T, req rpc.Request) {
	t.Helper()
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.in.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (b bridged) next(t *testing.T) rpc.Response {
	t.Helper()
	if !b.lines.Scan() {
		t.Fatalf("no line from the bridge: %v", b.lines.Err())
	}
	var resp rpc.Response
	if err := json.Unmarshal(b.lines.Bytes(), &resp); err != nil {
		t.Fatalf("%q: %v", b.lines.Text(), err)
	}
	return resp
}

func (b bridged) closeAndWait(t *testing.T) {
	t.Helper()
	_ = b.in.Close()
	select {
	case err := <-b.done:
		if err != nil {
			t.Fatalf("bridge ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not end after stdin closed")
	}
}

func TestRpcBridgeAnswersStatusWithTheBuildHandshake(t *testing.T) {
	socket, _ := startServeDaemon(t)
	b := startBridge(t, socket)
	b.send(t, rpc.Request{V: rpc.Version, ID: 1, Method: rpc.MethodStatus, Build: version.String()})
	resp := b.next(t)
	var st rpc.Status
	if resp.ID != 1 || resp.Error != nil || json.Unmarshal(resp.Result, &st) != nil || st.PID == 0 {
		t.Fatalf("status reply %+v", resp)
	}
	b.send(t, rpc.Request{V: rpc.Version, ID: 2, Method: rpc.MethodStatus, Build: "someone-else"})
	if resp := b.next(t); resp.ID != 2 || resp.Error == nil || resp.Error.Code != rpc.CodeVersionMismatch {
		t.Fatalf("mismatched build got %+v", resp)
	}
	b.closeAndWait(t)
}

func TestRpcBridgeStreamsASubscriptionUntilStdinCloses(t *testing.T) {
	socket, c := startServeDaemon(t)
	b := startBridge(t, socket)
	b.send(t, rpc.Request{V: rpc.Version, ID: 7, Method: rpc.MethodSubscribe, Build: version.String()})
	if resp := b.next(t); resp.ID != 7 || resp.Error != nil || len(resp.Result) == 0 {
		t.Fatalf("snapshot %+v", resp)
	}
	dir := t.TempDir()
	var ws rpc.Workspace
	if err := c.Call(t.Context(), rpc.MethodWorkspaceAdd, rpc.WorkspaceAddParams{Path: dir}, &ws); err != nil {
		t.Fatal(err)
	}
	if resp := b.next(t); resp.ID != 7 || resp.Diff == nil {
		t.Fatalf("diff %+v", resp)
	}
	b.closeAndWait(t)
}
