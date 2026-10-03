package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

// why: the agent waits on the hook, so a slow or dead daemon must never hold
// it longer than this.
const hookTimeout = 50 * time.Millisecond

// why: always exits 0, since a failing hook would surface as an error in the
// agent. Failures go to home/hook.log instead.
func runHook(args []string, stdin io.Reader, stdout io.Writer, home, pane string) int {
	if err := hook1(args, stdin, stdout, home, pane); err != nil {
		logHook(home, err)
	}
	return 0
}

func hook1(args []string, stdin io.Reader, stdout io.Writer, home, pane string) error {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	harness := fs.String("harness", "", "claude, codex or omp")
	event := fs.String("event", "", "the harness hook name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *harness == "" || *event == "" {
		return errors.New("usage: agentws hook --harness claude|codex|omp --event <name>")
	}
	payload, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	h := rpc.Hook{Harness: *harness, Event: *event, Pane: pane, At: time.Now()}
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return fmt.Errorf("%s %s: stdin is not JSON", *harness, *event)
		}
		h.Payload = payload
	}
	return sendHook(home, h, domain.HookWantsReply(*event), stdout)
}

func sendHook(home string, h rpc.Hook, wantReply bool, stdout io.Writer) error {
	deadline := time.Now().Add(hookTimeout)
	nc, err := net.DialTimeout("unix", rpc.SocketPath(home), hookTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = nc.Close() }()
	_ = nc.SetDeadline(deadline)
	params, err := json.Marshal(h)
	if err != nil {
		return err
	}
	req, err := json.Marshal(rpc.Request{V: rpc.Version, ID: 1, Method: rpc.MethodHook, Params: params})
	if err != nil {
		return err
	}
	if _, err := nc.Write(append(req, '\n')); err != nil {
		return err
	}
	if !wantReply {
		return nil
	}
	line, err := bufio.NewReader(nc).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("%s %s: no reply within %v: %w", h.Harness, h.Event, hookTimeout, err)
	}
	var resp rpc.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	var reply rpc.HookReply
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &reply); err != nil {
			return err
		}
	}
	if len(reply.Output) > 0 {
		_, err = fmt.Fprintf(stdout, "%s\n", reply.Output)
	}
	return err
}

func logHook(home string, cause error) {
	if home == "" {
		return
	}
	_ = os.MkdirAll(home, 0o700)
	f, err := os.OpenFile(filepath.Join(home, "hook.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), cause)
}
