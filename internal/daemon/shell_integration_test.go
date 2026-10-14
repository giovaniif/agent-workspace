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
	"github.com/giovaniif/crew/internal/adapters/nvim"
	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type liveTerm struct {
	c     *rpc.Client
	host  *tmux.Host
	home  string
	slot  app.Slot
	agent app.PaneID
	wtA   string
	wtB   string
}

func startLiveTerm(t *testing.T) liveTerm {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	home, err := filepath.EvalSymlinks(shortDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+name+"_HOME", filepath.Join(home, "xdg-"+strings.ToLower(name)))
	}
	l := liveTerm{home: home, wtA: filepath.Join(home, "wt", "api"), wtB: filepath.Join(home, "wt", "web")}
	for _, dir := range []string{l.wtA, l.wtB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	l.host = tmux.New(tmux.Config{Socket: fmt.Sprintf("crew-shell-%d-%d", os.Getpid(), time.Now().UnixNano()), ConfigPath: filepath.Join(home, "tmux.conf")})
	t.Cleanup(func() { _ = l.host.Close(context.Background()) })
	ctx := context.Background()
	l.agent, err = l.host.Create(ctx, app.PaneSpec{Name: "agent", Dir: l.wtA, Command: []string{"cat"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "s1", Pane: string(l.agent), Harness: domain.HarnessClaude}}
	store.snap.Worktrees = []domain.Worktree{
		{ID: "w-api", Path: l.wtA, SessionID: "s1"},
		{ID: "w-web", Path: l.wtB, SessionID: "s1"},
	}
	d, path := start(t, store,
		daemon.WithHarnesses(l.host, claude.Adapter{}),
		daemon.WithTerminals(home, nvim.Editor{}))
	d.SetClientHost(l.host)
	l.c = dial(t, path)
	opened, err := l.c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"cat"}})
	if err != nil {
		t.Fatal(err)
	}
	l.slot = app.Slot(opened.Slot)
	return l
}

func (l liveTerm) capture(t *testing.T, pane app.PaneID) string {
	t.Helper()
	out, err := l.host.Capture(context.Background(), pane, 50)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func waitUntilTrue(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestShellOpensInTheSelectedWorktreesPath(t *testing.T) {
	l := startLiveTerm(t)
	ctx := context.Background()
	for _, want := range []struct{ worktree, dir string }{{"w-web", l.wtB}, {"w-api", l.wtA}} {
		var out rpc.ShellResult
		if err := l.c.Call(ctx, rpc.MethodShellToggle, rpc.ShellParams{Session: "s1", Worktree: want.worktree}, &out); err != nil {
			t.Fatal(err)
		}
		if !out.Shown || out.Dir != want.dir {
			t.Fatalf("toggle %s = %+v; want a shell shown in %s", want.worktree, out, want.dir)
		}
		if got := l.host.BelowPane(ctx, l.slot); got != app.PaneID(out.Pane) {
			t.Fatalf("pane below the agent = %q; want the shell %s", got, out.Pane)
		}
		if err := l.host.SendText(ctx, app.PaneID(out.Pane), "echo cwd=$(pwd -P)", false); err != nil {
			t.Fatal(err)
		}
		if err := l.host.SendKeys(ctx, app.PaneID(out.Pane), "Enter"); err != nil {
			t.Fatal(err)
		}
		waitUntilTrue(t, "the shell to report its directory "+want.dir, func() bool {
			return strings.Contains(l.capture(t, app.PaneID(out.Pane)), "cwd="+want.dir+"\n") ||
				strings.HasSuffix(strings.TrimSpace(l.capture(t, app.PaneID(out.Pane))), "cwd="+want.dir)
		})
	}

	var hidden rpc.ShellResult
	if err := l.c.Call(ctx, rpc.MethodShellToggle, rpc.ShellParams{Session: "s1", Worktree: "w-api"}, &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Shown || l.host.BelowPane(ctx, l.slot) != "" {
		t.Fatalf("the second toggle left the shell open: %+v", hidden)
	}
	if alive, _ := l.host.Alive(ctx, app.PaneID(hidden.Pane)); !alive {
		t.Fatal("hiding the shell killed it")
	}
}

func TestNvimOpenShowsTheFileAtTheLineAndReusesTheSessionsNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	l := startLiveTerm(t)
	ctx := context.Background()
	file := filepath.Join(l.wtA, "main.go")
	if err := os.WriteFile(file, []byte("a\nb\nc\nd\ne\nf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(l.home, "nvim", "s1.sock")
	where := func() string {
		out, err := exec.Command("nvim", "--server", sock, "--remote-expr", `expand('%:p') . ':' . line('.')`).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	var first rpc.NvimResult
	if err := l.c.Call(ctx, rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-api", Path: "main.go", Line: 3}, &first); err != nil {
		t.Fatal(err)
	}
	waitUntilTrue(t, "nvim to open main.go at line 3", func() bool { return where() == file+":3" })
	if got := l.host.ShownIn(ctx, l.slot); got != app.PaneID(first.Pane) {
		t.Fatalf("the slot shows %q; want the nvim pane %s", got, first.Pane)
	}

	var second rpc.NvimResult
	if err := l.c.Call(ctx, rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-api", Path: "main.go", Line: 5}, &second); err != nil {
		t.Fatal(err)
	}
	waitUntilTrue(t, "the running nvim to jump to line 5", func() bool { return where() == file+":5" })
	if second.Pane != first.Pane {
		t.Fatalf("a second nvim was started: %s then %s", first.Pane, second.Pane)
	}

	var back rpc.NvimResult
	if err := l.c.Call(ctx, rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}, &back); err != nil {
		t.Fatal(err)
	}
	if back.Shown || l.host.ShownIn(ctx, l.slot) != l.agent {
		t.Fatalf("toggle = %+v, slot shows %q; want the agent pane back", back, l.host.ShownIn(ctx, l.slot))
	}
}
