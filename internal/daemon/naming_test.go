package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type namingRig struct {
	c      *rpc.Client
	titles *fakeTitles
	diffs  <-chan rpc.Diff
	path   string
}

func startNaming(t *testing.T, titles *fakeTitles) namingRig {
	t.Helper()
	store := &memStore{}
	store.snap.Workspaces = append(store.snap.Workspaces, singleWS)
	_, path := start(t, store,
		daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}),
		daemon.WithSessions(&fakeWorktrees{}, nil, "/h/worktrees"),
		daemon.WithTitles(titles))
	sub, err := dial(t, path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return namingRig{c: dial(t, path), titles: titles, diffs: sub.Diffs, path: path}
}

func (r namingRig) newSession(t *testing.T, workItem string) domain.Session {
	t.Helper()
	var s domain.Session
	p := rpc.NewSessionParams{Workspace: "/src/api", WorkItem: workItem, Harness: "claude"}
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (r namingRig) task(t *testing.T, id string) domain.Task {
	t.Helper()
	sub, err := dial(t, r.path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range sub.State.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("no task %s", id)
	return domain.Task{}
}

func nextTask(t *testing.T, diffs <-chan rpc.Diff, within time.Duration) domain.Task {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case d := <-diffs:
			if d.Task != nil && (d.Task.PRTitle != "") {
				return *d.Task
			}
		case <-deadline:
			t.Fatal("no resolved title in time")
		}
	}
}

func TestNamingPRSessionShowsThePRTitleWithinTwoSeconds(t *testing.T) {
	r := startNaming(t, &fakeTitles{title: "Fix the login redirect"})
	start := time.Now()
	s := r.newSession(t, "https://github.com/acme/api/pull/1")
	task := nextTask(t, r.diffs, 2*time.Second)
	if task.ID != s.TaskID || domain.NameFor(task, nil) != "Fix the login redirect" {
		t.Fatalf("task %+v", task)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v", took)
	}
	if got := r.titles.calls(); len(got) != 1 || got[0].Ref != "api#1" {
		t.Errorf("resolver asked %+v", got)
	}
}

func TestNamingPRSessionGetsThePRTitle(t *testing.T) {
	r := startNaming(t, &fakeTitles{title: "Add login"})
	r.newSession(t, "https://github.com/acme/api/pull/42")
	if task := nextTask(t, r.diffs, 2*time.Second); task.PRTitle != "Add login" {
		t.Fatalf("task %+v", task)
	}
}

func TestNamingTextSessionsAndRepeatsAreNotResolved(t *testing.T) {
	r := startNaming(t, &fakeTitles{title: "Fix the login redirect"})
	r.newSession(t, "tidy up the parser")
	r.newSession(t, "https://github.com/acme/api/pull/1")
	nextTask(t, r.diffs, 2*time.Second)
	r.newSession(t, "https://github.com/acme/api/pull/1")
	if err := r.c.Call(context.Background(), rpc.MethodStatus, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.titles.calls(); len(got) != 1 {
		t.Errorf("resolver asked %d times, want once for the one new work item", len(got))
	}
}

func TestNamingResolverFailureLeavesTheTitleEmpty(t *testing.T) {
	titles := &fakeTitles{err: errors.New("offline")}
	r := startNaming(t, titles)
	s := r.newSession(t, "https://github.com/acme/api/pull/1")
	deadline := time.Now().Add(2 * time.Second)
	for len(titles.calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := r.c.Call(context.Background(), rpc.MethodStatus, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.task(t, s.TaskID); got.PRTitle != "" {
		t.Errorf("task = %+v, want no title", got)
	}
}

func TestNamingRenamePinsAndUnpinDropsThePin(t *testing.T) {
	r := startNaming(t, &fakeTitles{})
	s := r.newSession(t, "tidy up the parser")

	if err := r.c.Call(context.Background(), rpc.MethodSessionRename, rpc.SessionRenameParams{ID: s.ID, Name: "  parser cleanup "}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.task(t, s.TaskID); got.PinnedName != "parser cleanup" || domain.NameFor(got, nil) != "parser cleanup" {
		t.Fatalf("after rename %+v", got)
	}
	if err := r.c.Call(context.Background(), rpc.MethodSessionUnpin, rpc.SessionRef{ID: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.task(t, s.TaskID); got.PinnedName != "" || domain.NameFor(got, nil) != "tidy up the parser" {
		t.Fatalf("after unpin %+v", got)
	}
}

func TestNamingRenameErrors(t *testing.T) {
	r := startNaming(t, &fakeTitles{})
	s := r.newSession(t, "tidy up the parser")
	var rerr *rpc.Error
	err := r.c.Call(context.Background(), rpc.MethodSessionRename, rpc.SessionRenameParams{ID: "nope", Name: "x"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v", err)
	}
	err = r.c.Call(context.Background(), rpc.MethodSessionRename, rpc.SessionRenameParams{ID: s.ID, Name: "  "}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("blank name: %v", err)
	}
	err = r.c.Call(context.Background(), rpc.MethodSessionUnpin, rpc.SessionRef{ID: "nope"}, nil)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unpin unknown session: %v", err)
	}
	if got := r.task(t, s.TaskID); got.PinnedName != "" {
		t.Errorf("a rejected rename pinned %+v", got)
	}
}

func TestNamingResolvedTitleDoesNotUndoAPinMadeMeanwhile(t *testing.T) {
	titles := &fakeTitles{title: "Fix the login redirect", gate: make(chan struct{})}
	r := startNaming(t, titles)
	s := r.newSession(t, "https://github.com/acme/api/pull/1")
	if err := r.c.Call(context.Background(), rpc.MethodSessionRename, rpc.SessionRenameParams{ID: s.ID, Name: "mine"}, nil); err != nil {
		t.Fatal(err)
	}
	close(titles.gate)
	task := nextTask(t, r.diffs, 2*time.Second)
	if task.PinnedName != "mine" || task.PRTitle != "Fix the login redirect" || domain.NameFor(task, nil) != "mine" {
		t.Fatalf("task %+v", task)
	}
}

func TestNamingASessionWithNoWorkItemTakesItsFirstPrompt(t *testing.T) {
	r := startNaming(t, &fakeTitles{title: "never"})
	s := r.newSession(t, "  ")
	h := rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: s.Pane, Payload: []byte(`{"prompt":"fix the flaky upload test"}`)}
	if err := r.c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
	other := r.newSession(t, "")
	if other.TaskID == s.TaskID {
		t.Fatal("two sessions with no work item share a task")
	}
	got := r.task(t, s.TaskID)
	if got.Text != "fix the flaky upload test" || domain.NameFor(got, nil) == "" {
		t.Fatalf("task %+v", got)
	}
	if err := r.c.Call(context.Background(), rpc.MethodHook, rpc.Hook{Harness: "claude", Event: "UserPromptSubmit", Pane: s.Pane, Payload: []byte(`{"prompt":"now do more"}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.task(t, s.TaskID); got.Text != "fix the flaky upload test" {
		t.Fatalf("a later prompt renamed the task: %+v", got)
	}
}
