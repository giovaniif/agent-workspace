//go:build integration

package integration

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
	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
	gitadapter "github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func serveReview(t *testing.T, home, socket, agent string) (*rpc.Client, func()) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	host := tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")})
	d, err := daemon.New(store, os.Getpid(),
		daemon.WithWorkspaces(wsfs.FS{}, gitadapter.Inspector{}),
		daemon.WithHarnesses(host, claude.Adapter{Binary: agent}),
		daemon.WithSessions(gitadapter.Adder{}, nil, filepath.Join(home, "worktrees")),
		daemon.WithWorktrees(gitadapter.Worktrees{}, noPRs{}),
		daemon.WithWorktreePoll(50*time.Millisecond, 0),
		daemon.WithReview(gitadapter.Review{}),
		daemon.WithHunks(gitadapter.Review{}))
	if err != nil {
		t.Fatal(err)
	}
	d.SetClientHost(host)
	path := filepath.Join(home, "crew.sock")
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
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = c.Close()
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return c, stop
}

type noPRs struct{}

func (noPRs) PRs(context.Context, []string) (map[string][]domain.PullRequest, error) {
	return nil, nil
}

func eventually(t *testing.T, within time.Duration, ok func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

func TestReviewSendReachesARealAgentPaneAcrossARestart(t *testing.T) {
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
	tmp, err := os.MkdirTemp("/tmp", "crew-r")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	home, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(home, "origin")
	repo := filepath.Join(home, "api")
	newRepo(t, origin)
	git(t, home, "clone", "-q", origin, repo)

	received := filepath.Join(home, "received")
	agent := filepath.Join(home, "fake-agent")
	script := fmt.Sprintf("#!/bin/sh\nstty -echo\nexec cat > %s\n", received)
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	socket := fmt.Sprintf("crew-it-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		_ = tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")}).Close(context.Background())
	})

	c, stop := serveReview(t, home, socket, agent)
	ctx := context.Background()
	if err := c.Call(ctx, rpc.MethodWorkspaceAdd, rpc.WorkspaceAddParams{Path: repo}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}
	var s domain.Session
	if err := c.Call(ctx, rpc.MethodNewSession, rpc.NewSessionParams{Workspace: repo, WorkItem: "https://github.com/acme/api/pull/42", Harness: "claude"}, &s); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(home, "worktrees", "api", "api-42")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var rv rpc.Review
	if !eventually(t, 5*time.Second, func() bool {
		rv, err = c.Review(ctx, rpc.ReviewParams{Session: s.ID, Scope: domain.ScopeUncommitted})
		return err == nil && len(rv.Worktrees) == 1 && rv.Worktrees[0].Worktree.Path == wt && len(rv.Worktrees[0].Files) == 2
	}) {
		t.Fatalf("review of %s = %+v, %v", wt, rv, err)
	}
	var want []domain.ReviewComment
	for _, f := range rv.Worktrees[0].Files {
		lines := f.Hunks[0].Lines
		cm := domain.CommentOn(wt, f.Path, lines[len(lines)-1:], "look at "+f.Path)
		want = append(want, cm)
		p := rpc.CommentParams{Session: s.ID, File: filepath.Join(wt, f.Path), StartLine: cm.Start, EndLine: cm.End, Code: strings.Join(cm.Code, "\n"), Body: cm.Body}
		if _, err := c.AddReviewComment(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	stop()
	c, _ = serveReview(t, home, socket, agent)
	rv, err = c.Review(ctx, rpc.ReviewParams{Session: s.ID, Scope: domain.ScopeUncommitted})
	if err != nil || len(rv.Draft.Comments) != 2 {
		t.Fatalf("after a restart the draft is %+v, %v", rv.Draft, err)
	}
	sent, err := c.SendReview(ctx, s.ID)
	if err != nil || sent.Status != domain.DraftSent {
		t.Fatalf("send = %+v, %v", sent, err)
	}
	var got string
	eventually(t, 5*time.Second, func() bool {
		b, _ := os.ReadFile(received)
		got = string(b)
		return strings.Contains(got, "look at main.go")
	})
	for _, line := range strings.Split(strings.TrimSpace(domain.ReviewPrompt(want)), "\n") {
		if !strings.Contains(got, line) {
			t.Errorf("the agent did not receive %q; got:\n%s", line, got)
		}
	}
}
