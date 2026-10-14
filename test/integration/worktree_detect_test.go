//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
	gitadapter "github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/github"
	"github.com/giovaniif/crew/internal/adapters/procs"
	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type env struct {
	root   string
	sock   string
	ghOut  string
	client *rpc.Client
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(path, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", ".")
	git(t, path, "commit", "-q", "-m", "init")
}

func start(t *testing.T, poll, prPoll time.Duration) env {
	t.Helper()
	for _, bin := range []string{"git", "sh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")

	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := env{root: filepath.Join(tmp, "ws"), ghOut: filepath.Join(tmp, "prs.json")}
	newRepo(t, filepath.Join(e.root, "api"))
	newRepo(t, filepath.Join(e.root, "web"))
	git(t, filepath.Join(e.root, "api"), "remote", "add", "origin", "https://github.com/o/api.git")
	git(t, filepath.Join(e.root, "web"), "remote", "add", "origin", "git@github.com:o/web.git")
	git(t, filepath.Join(e.root, "api"), "worktree", "add", "-q", "-b", "old", filepath.Join(e.root, "api-old"))

	gh := filepath.Join(tmp, "gh")
	script := "#!/bin/sh\ncat '" + e.ghOut + "' 2>/dev/null || echo '{\"data\":{}}'\n"
	if err := os.WriteFile(gh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(gh).CombinedOutput(); err != nil {
		t.Fatalf("fake gh: %v\n%s", err, out)
	}

	store, err := sqlite.Open(filepath.Join(tmp, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.PutSession(domain.Session{ID: "s1", Pane: "%1", State: domain.StateRunning})
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	d, err := daemon.New(store, os.Getpid(),
		daemon.WithWorkspaces(wsfs.FS{}, gitadapter.Inspector{}),
		daemon.WithWorktrees(gitadapter.Worktrees{}, &github.Finder{Bin: gh}),
		daemon.WithWorktreePoll(poll, prPoll),
		daemon.WithProcessTable(procs.Table{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	sockDir, err := os.MkdirTemp("/tmp", "crew-i")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	e.sock = filepath.Join(sockDir, "agentws.sock")
	ln, err := net.Listen("unix", e.sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	e.client, err = rpc.Dial(e.sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.client.Close() })
	if _, err := e.client.WorkspaceAdd(ctx, e.root); err != nil {
		t.Fatal(err)
	}
	w := e.waitFor(t, 5*time.Second, "api-old adopted", func(st rpc.State) bool {
		_, ok := find(st, filepath.Join(e.root, "api-old"))
		return ok
	})
	if got, _ := find(w, filepath.Join(e.root, "api-old")); got.SessionID != "" {
		t.Fatalf("pre-existing worktree = %+v, want unassigned", got)
	}
	return e
}

func (e env) state(t *testing.T) rpc.State {
	t.Helper()
	c, err := rpc.Dial(e.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sub.State
}

func (e env) waitFor(t *testing.T, within time.Duration, what string, ok func(rpc.State) bool) rpc.State {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		st := e.state(t)
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v; worktrees %+v", what, within, st.Worktrees)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func find(st rpc.State, path string) (domain.Worktree, bool) {
	for _, w := range st.Worktrees {
		if w.Path == path {
			return w, true
		}
	}
	return domain.Worktree{}, false
}

func (e env) agentRuns(t *testing.T, cwd string, args ...string) {
	t.Helper()
	git(t, cwd, args...)
	command := "git"
	for _, a := range args {
		command += " " + a
	}
	payload, _ := json.Marshal(map[string]any{"cwd": cwd, "tool_name": "Bash", "tool_input": map[string]string{"command": command}})
	h := rpc.Hook{Harness: "claude", Event: "PostToolUse", Pane: "%1", At: time.Now(), Payload: payload}
	if err := e.client.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeDetectAgentWorktreesAttachWithin2s(t *testing.T) {
	e := start(t, time.Hour, time.Hour)
	api, web := filepath.Join(e.root, "api"), filepath.Join(e.root, "web")
	want := []string{filepath.Join(e.root, "api-a"), filepath.Join(e.root, "api-b"), filepath.Join(e.root, "web-c")}
	e.agentRuns(t, api, "worktree", "add", "-q", "-b", "a", want[0])
	e.agentRuns(t, api, "worktree", "add", "-q", "-b", "b", want[1])
	e.agentRuns(t, web, "worktree", "add", "-q", "-b", "c", want[2])

	st := e.waitFor(t, 2*time.Second, "three worktrees on s1", func(st rpc.State) bool {
		for _, p := range want {
			if w, ok := find(st, p); !ok || w.SessionID != "s1" {
				return false
			}
		}
		return true
	})
	for _, s := range st.Sessions {
		if s.ID == "s1" && len(s.WorktreeIDs) != 3 {
			t.Errorf("s1 worktrees = %v", s.WorktreeIDs)
		}
	}
	if w, _ := find(st, filepath.Join(e.root, "api-old")); w.SessionID != "" {
		t.Errorf("api-old = %+v, want still unassigned", w)
	}
}

func TestWorktreeDetectHandMadeWorktreeUnassignedWithin15s(t *testing.T) {
	e := start(t, daemon.DefaultWorktreePoll, time.Hour)
	hand := filepath.Join(e.root, "web-hand")
	git(t, filepath.Join(e.root, "web"), "worktree", "add", "-q", "-b", "hand", hand)
	st := e.waitFor(t, 15*time.Second, "hand-made worktree", func(st rpc.State) bool {
		_, ok := find(st, hand)
		return ok
	})
	if w, _ := find(st, hand); w.SessionID != "" || w.Branch != "hand" {
		t.Errorf("hand-made = %+v, want unassigned on branch hand", w)
	}
}

func TestWorktreeDetectPRAndChecksWithinOnePoll(t *testing.T) {
	const prPoll = 300 * time.Millisecond
	e := start(t, time.Hour, prPoll)
	prs := `{"data":{"r0":{"pullRequests":{"nodes":[{"number":42,"title":"Old","url":"https://github.com/o/api/pull/42","headRefName":"old","state":"OPEN",
	  "reviewDecision":"REVIEW_REQUIRED","mergeable":"MERGEABLE",
	  "reviewThreads":{"nodes":[{"isResolved":false,"comments":{"nodes":[]}}]},"comments":{"nodes":[]},"reviews":{"nodes":[]},
	  "commits":{"nodes":[{"commit":{"committedDate":"2026-09-01T00:00:00Z","statusCheckRollup":{"contexts":{"nodes":[
	    {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://github.com/o/api/actions/runs/1"}]}}}}]}}]}}}}`
	if err := os.WriteFile(e.ghOut, []byte(prs), 0o644); err != nil {
		t.Fatal(err)
	}
	e.waitFor(t, 2*prPoll+500*time.Millisecond, "PR on api-old", func(st rpc.State) bool {
		w, _ := find(st, filepath.Join(e.root, "api-old"))
		return w.PR != nil && w.PR.Number == 42 && w.PR.Checks == domain.CheckFailing &&
			w.PR.UnresolvedThreads == 1 && len(w.PR.Failing) == 1 && w.PR.Failing[0].Name == "test"
	})
}

func TestWorktreeDetectRemovedOutsideTheToolDropsOnNextPoll(t *testing.T) {
	const poll = 300 * time.Millisecond
	e := start(t, poll, time.Hour)
	api := filepath.Join(e.root, "api")
	gone := filepath.Join(e.root, "api-gone")
	git(t, api, "worktree", "add", "-q", "-b", "gone", gone)
	e.waitFor(t, 2*time.Second, "api-gone seen", func(st rpc.State) bool {
		_, ok := find(st, gone)
		return ok
	})

	git(t, api, "worktree", "remove", filepath.Join(e.root, "api-old"))
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	e.waitFor(t, 2*poll+500*time.Millisecond, "both dropped", func(st rpc.State) bool {
		_, old := find(st, filepath.Join(e.root, "api-old"))
		_, g := find(st, gone)
		return !old && !g
	})
}
