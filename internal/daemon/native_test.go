package daemon_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestNativeClientHandsOutTheControlModeArgvAndMapsPanesToSessions(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{native: app.NativeClient{
		Session: "agentws-native-1",
		Argv:    []string{"tmux", "-L", "agentws", "-C", "attach-session", "-t", "=agentws-native-1"},
		Panes: []app.NativePane{
			{Pane: "%3", Window: "@2", Cols: 120, Rows: 40, MouseAny: true, MouseSGR: true},
			{Pane: "%9", Window: "@5", Cols: 120, Rows: 40, Alternate: true},
		},
	}}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)

	got, err := c.NativeClient(ctx, rpc.NativeClientParams{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != "agentws-native-1" || !slices.Equal(got.Argv, host.native.Argv) {
		t.Fatalf("reply = %+v; want the host's session and argv", got)
	}
	want := []rpc.NativePane{
		{Pane: "%3", Window: "@2", SessionID: "a", Cols: 120, Rows: 40, MouseAny: true, MouseSGR: true},
		{Pane: "%9", Window: "@5", Cols: 120, Rows: 40, Alternate: true},
	}
	if !slices.Equal(got.Panes, want) {
		t.Fatalf("panes = %+v; want %+v", got.Panes, want)
	}
	if !slices.Equal(host.nativeSizes, [][2]int{{120, 40}}) {
		t.Fatalf("host sized %v; want 120x40", host.nativeSizes)
	}
}

func TestNativeClientNeedsASize(t *testing.T) {
	d, path := start(t, &memStore{})
	d.SetClientHost(&fakeClientHost{})
	var rerr *rpc.Error
	_, err := dial(t, path).NativeClient(context.Background(), rpc.NativeClientParams{Cols: 0, Rows: 40})
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("err = %v; want %s", err, rpc.CodeBadRequest)
	}
}

func TestNativeClientWithoutATerminalHostIsUnavailable(t *testing.T) {
	_, path := start(t, &memStore{})
	var rerr *rpc.Error
	_, err := dial(t, path).NativeClient(context.Background(), rpc.NativeClientParams{Cols: 80, Rows: 24})
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable {
		t.Fatalf("err = %v; want %s", err, rpc.CodeUnavailable)
	}
}
