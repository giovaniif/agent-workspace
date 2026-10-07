package daemon_test

import (
	"context"
	"slices"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func startProjectSessions(t *testing.T, projects []domain.Project) (*rpc.Client, *fakeRunner) {
	t.Helper()
	store := &memStore{}
	store.snap.Workspaces = []domain.Workspace{singleWS, orchWS}
	store.snap.Projects = projects
	runner := &fakeRunner{}
	_, path := start(t, store,
		daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}),
		daemon.WithSessions(&fakeWorktrees{}, nil, "/h/worktrees"),
		daemon.WithProjectSetup(runner))
	return dial(t, path), runner
}

func TestNewSessionInAProjectRunsItsSetupScriptInTheNewWorktree(t *testing.T) {
	c, runner := startProjectSessions(t, []domain.Project{{Root: "/src/api", Name: "api", Setup: "make env"}})
	params := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "fix login", Harness: "claude"}
	var got domain.Session
	if err := c.Call(context.Background(), rpc.MethodNewSession, params, &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/h/worktrees/api/fix-login: sh -c make env"}; !slices.Equal(runner.ran(), want) {
		t.Fatalf("ran %v, want %v", runner.ran(), want)
	}
}

func TestNewSessionOutsideAProjectRunsNoScript(t *testing.T) {
	c, runner := startProjectSessions(t, []domain.Project{{Root: "/src/shop", Name: "shop", Setup: "make env"}})
	params := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: "fix login", Harness: "claude"}
	var got domain.Session
	if err := c.Call(context.Background(), rpc.MethodNewSession, params, &got); err != nil {
		t.Fatal(err)
	}
	if ran := runner.ran(); len(ran) != 0 {
		t.Fatalf("ran %v", ran)
	}
}
