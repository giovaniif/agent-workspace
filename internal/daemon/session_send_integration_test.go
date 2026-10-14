//go:build integration

package daemon_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
)

func TestSessionSendAndInterruptReachARealTmuxPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	home, err := filepath.EvalSymlinks(shortDir(t))
	if err != nil {
		t.Fatal(err)
	}
	host := tmux.New(tmux.Config{Socket: fmt.Sprintf("crew-send-%d-%d", os.Getpid(), time.Now().UnixNano()), ConfigPath: filepath.Join(home, "tmux.conf")})
	t.Cleanup(func() { _ = host.Close(context.Background()) })
	ctx := context.Background()
	pane, err := host.Create(ctx, app.PaneSpec{Name: "agent", Dir: home, Command: []string{"cat", "-v"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "s1", Pane: string(pane), Harness: domain.HarnessClaude, State: domain.StateIdle}}
	_, path := start(t, store, daemon.WithHarnesses(host, claude.Adapter{}))
	c := dial(t, path)
	capture := func() string {
		out, err := host.Capture(ctx, pane, 50)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	sent, err := c.SessionSend(ctx, "s1", "hello from the phone")
	if err != nil || sent.Queued {
		t.Fatalf("send = %+v, %v; want pasted at once", sent, err)
	}
	waitUntilTrue(t, "the pasted line echoed back by cat", func() bool {
		return strings.Count(capture(), "hello from the phone") == 2
	})

	if err := c.SessionInterrupt(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	waitUntilTrue(t, "Escape in the pane", func() bool { return strings.Contains(capture(), "^[") })
}
