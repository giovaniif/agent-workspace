//go:build integration

package daemon_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
	gitadapter "github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cloneRepo(t *testing.T, scratch, dst string) {
	t.Helper()
	origin := filepath.Join(scratch, filepath.Base(dst)+"-origin")
	gitIn(t, scratch, "init", "-q", "-b", "main", origin)
	if err := os.WriteFile(filepath.Join(origin, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "add", ".")
	gitIn(t, origin, "commit", "-qm", "one")
	gitIn(t, scratch, "clone", "-q", origin, dst)
	gitIn(t, dst, "commit", "-q", "--allow-empty", "-m", "local")
}

type liveDaemon struct {
	c      *rpc.Client
	path   string
	cancel func()
}

func serveLive(t *testing.T, home, socket, fakeAgent string, extra ...daemon.Option) liveDaemon {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	host := tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")})
	d, err := daemon.New(store, os.Getpid(), append([]daemon.Option{
		daemon.WithWorkspaces(wsfs.FS{}, gitadapter.Inspector{}),
		daemon.WithHarnesses(host, claude.Adapter{Binary: fakeAgent}, codex.Adapter{Binary: fakeAgent}),
		daemon.WithSessions(gitadapter.Adder{}, nil, filepath.Join(home, "worktrees")),
		daemon.WithWorktrees(gitadapter.Worktrees{}, noPRs{}),
		daemon.WithWorktreePoll(50*time.Millisecond, 0)}, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	d.SetClientHost(host)
	path := filepath.Join(home, "agentws.sock")
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Serve(ctx, ln)
		_ = store.Close()
		close(done)
	}()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := func() {
		_ = c.Close()
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return liveDaemon{c: c, path: path, cancel: stop}
}

func (l liveDaemon) sessions(t *testing.T) map[string]domain.Session {
	t.Helper()
	c, err := rpc.Dial(l.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]domain.Session{}
	for _, s := range sub.State.Sessions {
		out[s.ID] = s
	}
	return out
}

func readCwd(t *testing.T, file string) string {
	t.Helper()
	var b []byte
	waitFor(t, func() bool {
		var err error
		b, err = os.ReadFile(file)
		return err == nil && len(b) > 0
	})
	return strings.TrimSpace(string(b))
}

func TestNewSessionCwdAndWorktreeLayoutPerWorkspaceKindAndRestart(t *testing.T) {
	for _, bin := range []string{"git", "tmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@example.com"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@example.com"}} {
		t.Setenv(kv[0], kv[1])
	}
	home, err := filepath.EvalSymlinks(shortDir(t))
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(home, "src")
	if err := os.MkdirAll(filepath.Join(scratch, "shop"), 0o755); err != nil {
		t.Fatal(err)
	}
	single := filepath.Join(scratch, "api")
	cloneRepo(t, scratch, single)
	shop := filepath.Join(scratch, "shop")
	cloneRepo(t, scratch, filepath.Join(shop, "web"))

	cwds := filepath.Join(home, "cwds")
	if err := os.MkdirAll(cwds, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeAgent := filepath.Join(home, "fake-agent")
	script := fmt.Sprintf("#!/bin/sh\npwd -P > %s/$$\nexec cat\n", cwds)
	if err := os.WriteFile(fakeAgent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	socket := fmt.Sprintf("crew-it-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		_ = tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")}).Close(context.Background())
	})

	live := serveLive(t, home, socket, fakeAgent)
	ctx := context.Background()
	for _, root := range []string{single, shop} {
		if err := live.c.Call(ctx, rpc.MethodWorkspaceAdd, rpc.WorkspaceAddParams{Path: root}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		var list rpc.WorkspaceList
		_ = live.c.Call(ctx, rpc.MethodWorkspaceList, nil, &list)
		for _, w := range list.Workspaces {
			if w.Root == single && len(w.Repos) == 1 && w.Repos[0].DefaultBranch == "main" {
				return true
			}
		}
		return false
	})
	if _, err := live.c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}

	began := time.Now()
	var one domain.Session
	if err := live.c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: single, WorkItem: "https://github.com/acme/api/pull/42", Harness: "claude"}, &one); err != nil {
		t.Fatal(err)
	}
	if err := live.c.Call(ctx, rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: one.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("new session to visible pane took %v, over the 1 s budget", took)
	}
	wt := filepath.Join(home, "worktrees", "api", "api-42")
	if got := gitIn(t, wt, "rev-parse", "HEAD"); got != gitIn(t, single, "rev-parse", "origin/main") {
		t.Fatalf("worktree HEAD %s is not origin/main", got)
	}
	if got := gitIn(t, wt, "symbolic-ref", "--short", "HEAD"); got != "api-42" {
		t.Fatalf("branch %s", got)
	}
	time.Sleep(300 * time.Millisecond)
	var worktrees []domain.Worktree
	sub, err := dial(t, live.path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range sub.State.Worktrees {
		if w.Path == wt {
			worktrees = append(worktrees, w)
		}
	}
	if len(worktrees) != 1 || worktrees[0].ID != wt || worktrees[0].SessionID != one.ID || worktrees[0].Repo != single {
		t.Fatalf("after a worktree scan the new worktree is %+v, want one owned by %s", worktrees, one.ID)
	}

	var two domain.Session
	if err := live.c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: shop, WorkItem: "tidy the shop", Harness: "codex"}, &two); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(gitIn(t, filepath.Join(shop, "web"), "worktree", "list"), "\n")); n != 1 {
		t.Fatalf("orchestration root got %d worktrees in web", n)
	}
	if _, err := os.Stat(filepath.Join(home, "worktrees", "web")); !os.IsNotExist(err) {
		t.Fatalf("orchestration root made a worktree dir: %v", err)
	}

	var seen []string
	waitFor(t, func() bool {
		entries, _ := os.ReadDir(cwds)
		return len(entries) == 2
	})
	entries, _ := os.ReadDir(cwds)
	for _, e := range entries {
		seen = append(seen, readCwd(t, filepath.Join(cwds, e.Name())))
	}
	if !(contains(seen, wt) && contains(seen, shop)) {
		t.Fatalf("agents started in %v, want %s and %s", seen, wt, shop)
	}

	if err := live.c.Call(ctx, rpc.MethodHook, rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: one.Pane}, nil); err != nil {
		t.Fatal(err)
	}
	before := live.sessions(t)
	if before[one.ID].State != domain.StateRunning || before[two.ID].State != domain.StateIdle {
		t.Fatalf("states before restart %+v", before)
	}
	reopened := live.sessions(t)
	if !sameSessions(before, reopened) {
		t.Fatalf("a second client sees %+v, the first %+v", reopened, before)
	}

	live.cancel()
	restarted := serveLive(t, home, socket, fakeAgent)
	time.Sleep(200 * time.Millisecond)
	if after := restarted.sessions(t); !sameSessions(before, after) {
		t.Fatalf("after a daemon restart %+v, before %+v", after, before)
	}

	var ended domain.Session
	if err := restarted.c.Call(ctx, rpc.MethodEndSession, rpc.SessionRef{ID: two.ID}, &ended); err != nil {
		t.Fatal(err)
	}
	if ended.State != domain.StateIdle || ended.Pane != "" {
		t.Fatalf("ended %+v", ended)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("ending a session touched its worktree: %v", err)
	}

	restarted.cancel()
	_ = tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")}).Close(ctx)
	afterReboot := serveLive(t, home, socket, fakeAgent)
	waitFor(t, func() bool {
		s := afterReboot.sessions(t)[one.ID]
		return s.State == domain.StateIdle && s.Pane == ""
	})
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sameSessions(a, b map[string]domain.Session) bool {
	if len(a) != len(b) {
		return false
	}
	for id, s := range a {
		o, ok := b[id]
		if !ok || o.State != s.State || o.Pane != s.Pane || o.TaskID != s.TaskID || o.Harness != s.Harness {
			return false
		}
	}
	return true
}
