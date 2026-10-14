package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func startPromptRig(t *testing.T, grace time.Duration) sessionRig {
	t.Helper()
	r := sessionRig{host: &fakeHost{}, wts: &fakeWorktrees{}}
	store := &memStore{}
	store.snap.Workspaces = append(store.snap.Workspaces, orchWS)
	r.d, r.path = start(t, store,
		daemon.WithHarnesses(r.host, claude.Adapter{}),
		daemon.WithSessions(r.wts, nil, "/h/worktrees"),
		daemon.WithFirstPromptGrace(grace),
	)
	r.c = dial(t, r.path)
	return r
}

func newWithPrompt(t *testing.T, r sessionRig, prompt string) (domain.Session, error) {
	t.Helper()
	var s domain.Session
	p := rpc.NewSessionParams{Workspace: "/src/shop", WorkItem: "tidy up", Harness: "claude", Prompt: prompt}
	err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &s)
	return s, err
}

func startHook(t *testing.T, r sessionRig, event string) {
	t.Helper()
	h := rpc.Hook{Harness: "claude", Event: event, Pane: "%7", At: time.Now(), Payload: []byte(`{}`)}
	if err := r.c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func pastedOn7(text string) []string {
	return []string{"%7 paste=true " + text, "%7 keys Enter"}
}

func TestNewSessionPromptWaitsForTheHarnessThenIsPastedAsTheFirstTurn(t *testing.T) {
	r := startPromptRig(t, time.Hour)
	if _, err := newWithPrompt(t, r, "fix the build"); err != nil {
		t.Fatal(err)
	}
	if got := queuedTexts(t, r.c); !reflect.DeepEqual(got, []string{"fix the build"}) {
		t.Fatalf("queued %q", got)
	}
	assertTypedStays(t, sendEnv{host: r.host}, nil)
	startHook(t, r, "SessionStart")
	if typed := r.host.waitTyped(t, 2); !reflect.DeepEqual(typed, pastedOn7("fix the build")) {
		t.Fatalf("typed %q", typed)
	}
	waitUntil(t, "queue emptied", func() bool { return len(queuedTexts(t, r.c)) == 0 })
}

func TestNewSessionPromptIsPastedAfterTheGraceWhenNoHookArrives(t *testing.T) {
	r := startPromptRig(t, 30*time.Millisecond)
	if _, err := newWithPrompt(t, r, "fix the build"); err != nil {
		t.Fatal(err)
	}
	if typed := r.host.waitTyped(t, 2); !reflect.DeepEqual(typed, pastedOn7("fix the build")) {
		t.Fatalf("typed %q", typed)
	}
}

func TestNewSessionPromptBlankOrAbsentQueuesNothing(t *testing.T) {
	r := startPromptRig(t, 20*time.Millisecond)
	if _, err := newWithPrompt(t, r, "   "); err != nil {
		t.Fatal(err)
	}
	if _, err := newWithPrompt(t, r, ""); err != nil {
		t.Fatal(err)
	}
	startHook(t, r, "SessionStart")
	assertTypedStays(t, sendEnv{host: r.host}, nil)
	if got := queuedTexts(t, r.c); len(got) != 0 {
		t.Fatalf("queued %q", got)
	}
}

func TestNewSessionPromptFailedLaunchQueuesNothingAndSendsNothing(t *testing.T) {
	r := startPromptRig(t, 20*time.Millisecond)
	r.host.mu.Lock()
	r.host.err = errors.New("tmux down")
	r.host.mu.Unlock()
	if _, err := newWithPrompt(t, r, "fix the build"); err == nil {
		t.Fatal("want the launch error")
	}
	assertTypedStays(t, sendEnv{host: r.host}, nil)
	if got := queuedTexts(t, r.c); len(got) != 0 {
		t.Fatalf("queued %q", got)
	}
}

func TestNewSessionPromptIsDroppedWithItsSessionBeforeTheHarnessStarts(t *testing.T) {
	r := startPromptRig(t, time.Hour)
	s, err := newWithPrompt(t, r, "fix the build")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.Call(context.Background(), rpc.MethodEndSession, rpc.SessionRef{ID: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got := queuedTexts(t, r.c); len(got) != 0 {
		t.Fatalf("queued %q", got)
	}
}

func TestNewSessionPromptNamesASessionWithNoWorkItem(t *testing.T) {
	r := startPromptRig(t, time.Hour)
	var s domain.Session
	p := rpc.NewSessionParams{Workspace: "/src/shop", Harness: "claude", Prompt: "fix the flaky upload test"}
	if err := r.c.Call(context.Background(), rpc.MethodNewSession, p, &s); err != nil {
		t.Fatal(err)
	}
	for _, task := range r.state(t).Tasks {
		if task.ID == s.TaskID && task.Text == "fix the flaky upload test" {
			return
		}
	}
	t.Fatalf("tasks %+v", r.state(t).Tasks)
}
