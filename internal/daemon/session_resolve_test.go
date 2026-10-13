package daemon_test

import (
	"context"
	"errors"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func resolve(t *testing.T, r namingRig, workspace, item string) (rpc.WorkItemResolved, error) {
	t.Helper()
	var got rpc.WorkItemResolved
	err := r.c.Call(context.Background(), rpc.MethodSessionResolve, rpc.ResolveWorkItemParams{Workspace: workspace, WorkItem: item}, &got)
	return got, err
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

func TestResolveWorkItemNamesTheTitleAndTheWorktree(t *testing.T) {
	titles := &fakeTitles{title: "Fix the login redirect"}
	r := startNaming(t, titles)
	got, err := resolve(t, r, "/src/api", "https://github.com/acme/api/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.WorkItemResolved{Source: "pr", Ref: "api#1", Title: "Fix the login redirect", Worktree: "api-1", Workspace: "/src/api"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestResolveWorkItemTextNeedsNoLookup(t *testing.T) {
	titles := &fakeTitles{title: "never"}
	r := startNaming(t, titles)
	got, err := resolve(t, r, "/src/api", "fix the login redirect today please")
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.WorkItemResolved{Source: "text", Title: "fix the login redirect today please", Worktree: "fix-the-login-redirect-today", Workspace: "/src/api"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(titles.calls()) != 0 {
		t.Fatalf("looked up %+v", titles.calls())
	}
}

func TestResolveWorkItemRejectsBadInput(t *testing.T) {
	r := startNaming(t, &fakeTitles{title: "t"})
	_, err := resolve(t, r, "/src/api", "  ")
	wantCode(t, err, rpc.CodeBadRequest)
	_, err = resolve(t, r, "/src/api", "https://github.com/acme/web/issues/4")
	wantCode(t, err, rpc.CodeBadRequest)
	_, err = resolve(t, r, "/nope", "x")
	wantCode(t, err, rpc.CodeNotFound)
}

func TestResolveWorkItemReportsAnItemTheTrackerDoesNotKnow(t *testing.T) {
	r := startNaming(t, &fakeTitles{err: errors.New("issue not found")})
	_, err := resolve(t, r, "/src/api", "https://github.com/acme/web/pull/404")
	wantCode(t, err, rpc.CodeNotFound)
	var rerr *rpc.Error
	errors.As(err, &rerr)
	if rerr.Message != "web#404: issue not found" {
		t.Fatalf("message %q", rerr.Message)
	}
}

func TestResolveWorkItemSkipsAWorktreeNameAlreadyTaken(t *testing.T) {
	titles := &fakeTitles{title: "Fix the login redirect"}
	r := startNaming(t, titles)
	r.newSession(t, "https://github.com/acme/api/pull/1")
	got, err := resolve(t, r, "/src/api", "https://github.com/acme/api/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Worktree != "api-1-2" {
		t.Fatalf("worktree %q", got.Worktree)
	}
}
