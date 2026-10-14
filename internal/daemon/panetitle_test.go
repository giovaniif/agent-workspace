package daemon_test

import (
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestAgentPanesGetATitleThatFollowsTheirSession(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateIdle, WorktreeIDs: []string{"w"}}}
	store.snap.Worktrees = []domain.Worktree{{ID: "w", Repo: "/src/api", SubtaskSlug: "x", SessionID: "a"}}
	r := startSessions(t, store, nil)
	waitUntil(t, "the title to be set", func() bool { return r.host.title("%3") == "○ claude │ api:x no PR" })

	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateRunning, WorktreeIDs: []string{"w"}}})
	waitUntil(t, "the title to follow the state", func() bool { return r.host.title("%3") == "◐ claude │ api:x no PR" })
	if n := r.host.titleSets(app.PaneID("%3")); n != 2 {
		t.Fatalf("title set %d times; want once per change", n)
	}
}

func TestTheShellPaneGetsATitle(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	out, err := r.toggle(t, rpc.ShellParams{Session: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.term.title(app.PaneID(out.Pane)); got != "shell · w-api · /wt/api · t hide · s type · T popup" {
		t.Fatalf("shell title %q", got)
	}
}
