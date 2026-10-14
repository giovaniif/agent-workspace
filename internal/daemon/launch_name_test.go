package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestNotifyLaunchedSessionBannerIsTitledWithItsName(t *testing.T) {
	n := newFakeNotifier()
	_, path := start(t, &memStore{},
		daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}, codex.Adapter{}),
		daemon.WithNotifier(n, nil, nil))
	c := dial(t, path)
	var got domain.Session
	params := rpc.LaunchParams{Harness: "codex", Name: "banner-check-codex", Dir: "/w"}
	if err := c.Call(context.Background(), rpc.MethodLaunch, params, &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID == "" {
		t.Fatal("named launch made no task")
	}
	for _, ev := range []string{"UserPromptSubmit", "Stop"} {
		h := rpc.Hook{Harness: "codex", Event: ev, Pane: got.Pane}
		if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case b := <-n.banners:
		if b.Title != "banner-check-codex" || b.Body != "done" {
			t.Fatalf("banner %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no banner")
	}
}

func TestLaunchWithoutANameMakesNoTask(t *testing.T) {
	_, path := start(t, &memStore{}, daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}))
	c := dial(t, path)
	var got domain.Session
	if err := c.Call(context.Background(), rpc.MethodLaunch, rpc.LaunchParams{Harness: "claude", Dir: "/w"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "" {
		t.Fatalf("task %q", got.TaskID)
	}
}
