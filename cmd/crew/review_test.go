package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestReviewCommentArgsBecomeCommentParams(t *testing.T) {
	t.Setenv("AGENTWS_SESSION", "from-env")
	got, err := parseCommentArgs([]string{"--file", "/wt/api/a.go", "--start", "3", "--end", "5", "--code", "x := 1", "--body", "why?"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.CommentParams{Session: "from-env", File: "/wt/api/a.go", StartLine: 3, EndLine: 5, Code: "x := 1", Body: "why?"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	got, err = parseCommentArgs([]string{"--session", "s2", "--worktree", "w1", "--path", "b.go", "--start", "7", "--body", "rename"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want := (rpc.CommentParams{Session: "s2", Worktree: "w1", Path: "b.go", StartLine: 7, Body: "rename"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReviewCommentNeedsASessionAFileALineAndABody(t *testing.T) {
	t.Setenv("AGENTWS_SESSION", "")
	cases := map[string][]string{
		"no session": {"--file", "/a.go", "--start", "1", "--body", "x"},
		"no file":    {"--session", "s", "--start", "1", "--body", "x"},
		"no line":    {"--session", "s", "--file", "/a.go", "--body", "x"},
		"no body":    {"--session", "s", "--file", "/a.go", "--start", "1"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(append([]string{"review", "comment"}, args...), io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: agentws review comment") {
				t.Fatalf("exit %d, stderr %q", code, stderr.String())
			}
		})
	}
}

func TestReviewCommentLandsInTheDaemonsDraft(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	d, err := daemon.New(nopStore{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s1"}})
	d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: "w1", Path: "/wt/api", SessionID: "s1"}})
	<-sub.Diffs
	<-sub.Diffs

	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := run([]string{"review", "comment", "--session", "s1", "--file", "/wt/api/pkg/a.go", "--start", "4", "--body", "nit"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	select {
	case diff := <-sub.Diffs:
		if diff.Comment == nil || diff.Comment.Path != "pkg/a.go" || diff.Comment.Worktree != "/wt/api" || diff.Comment.Body != "nit" {
			t.Fatalf("diff %+v", diff)
		}
		if !strings.Contains(stdout.String(), diff.Comment.ID) {
			t.Fatalf("stdout %q does not name comment %s", stdout.String(), diff.Comment.ID)
		}
	case <-time.After(time.Second - time.Since(started)):
		t.Fatal("the comment did not reach the draft within 1 s")
	}

	stderr.Reset()
	if code := run([]string{"review", "comment", "--session", "s1", "--file", "/elsewhere/a.go", "--start", "1", "--body", "x"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "none of the session's worktrees") {
		t.Fatalf("comment outside the worktrees: exit %d, stderr %q", code, stderr.String())
	}
}

func TestReviewScopeWithoutAFlagLeavesTheScopeToTheDaemon(t *testing.T) {
	t.Setenv("AGENTWS_SESSION", "s1")
	got, err := parseScopeArgs(nil, io.Discard)
	if err != nil || got != (rpc.ReviewParams{Session: "s1"}) {
		t.Fatalf("got %+v, %v; want only the session, so the daemon uses the TUI's scope", got, err)
	}
}

func TestReviewScopePrintsEachWorktreesBaseAndFiles(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	t.Setenv("AGENTWS_SESSION", "s1")
	var asked rpc.ReviewParams
	fakeDaemon(t, home, func(req rpc.Request) *rpc.Response {
		if req.Method != rpc.MethodReviewOpen {
			return nil
		}
		_ = json.Unmarshal(req.Params, &asked)
		body, _ := json.Marshal(rpc.Review{Scope: asked.Scope, Worktrees: []domain.WorktreeReview{
			{Worktree: domain.Worktree{ID: "w1", Path: "/wt/api"}, From: "abc123", Files: []domain.FileDiff{{Path: "a.go"}, {Path: "b/c.go"}}},
			{Worktree: domain.Worktree{ID: "w2", Path: "/wt/web"}, Err: "no turn yet"},
		}})
		return &rpc.Response{V: rpc.Version, ID: req.ID, Result: body}
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"review", "scope", "--scope", "last_turn"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if asked.Session != "s1" || asked.Scope != domain.ScopeLastTurn {
		t.Fatalf("asked %+v", asked)
	}
	var got []struct {
		Worktree string   `json:"worktree"`
		Path     string   `json:"path"`
		From     string   `json:"from"`
		Files    []string `json:"files"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}
	if len(got) != 2 || got[0].Worktree != "w1" || got[0].Path != "/wt/api" || got[0].From != "abc123" || strings.Join(got[0].Files, ",") != "a.go,b/c.go" || got[1].Error != "no turn yet" {
		t.Fatalf("got %+v", got)
	}
}
