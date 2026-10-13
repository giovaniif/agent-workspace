package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const runAutomations = `
[[automation]]
name = "fix"
agent = "claude"
prompt = "Fix {{work_item}}."

[[automation]]
name = "guarded"
agent = "claude"
prompt = "Guarded {{work_item}}."
precheck = "test -f go.mod"

[[automation]]
name = "again"
agent = "claude"
prompt = "Again {{context}}"
session = "reuse"
`

type automationRig struct {
	sessionRig
	store  *memStore
	runner *fakeRunner
}

func startAutomationRig(t *testing.T) automationRig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(runAutomations), 0o600); err != nil {
		t.Fatal(err)
	}
	r := automationRig{sessionRig: sessionRig{host: &fakeHost{}, wts: &fakeWorktrees{}}, store: &memStore{}, runner: &fakeRunner{}}
	r.store.snap.Workspaces = append(r.store.snap.Workspaces, orchWS)
	r.d, r.path = start(t, r.store,
		daemon.WithHarnesses(r.host, claude.Adapter{}),
		daemon.WithSessions(r.wts, nil, "/h/worktrees"),
		daemon.WithFirstPromptGrace(time.Hour),
		daemon.WithConfig(path),
		daemon.WithAutomationPrecheck(r.runner),
	)
	r.c = dial(t, r.path)
	return r
}

func runAutomation(t *testing.T, r automationRig, p rpc.AutomationRunParams) rpc.AutomationRunResult {
	t.Helper()
	var got rpc.AutomationRunResult
	if err := r.c.Call(context.Background(), rpc.MethodAutomationRun, p, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func liveSessions(t *testing.T, c *rpc.Client) int {
	t.Helper()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(sub.State.Sessions)
}

func TestAutomationRunWithContextStartsASessionWhoseFirstPromptHasIt(t *testing.T) {
	r := startAutomationRig(t)
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "fix", Context: "only the lint job", WorkItem: "tidy up", Repo: "/src/shop"})
	if got.Run.Outcome != domain.AutomationSucceeded || got.Run.SessionID == "" || got.Run.Trigger != domain.AutomationTriggerManual {
		t.Fatalf("run = %+v", got.Run)
	}
	if got.Session.ID != got.Run.SessionID {
		t.Errorf("session %q, run names %q", got.Session.ID, got.Run.SessionID)
	}
	if texts := queuedTexts(t, r.c); !reflect.DeepEqual(texts, []string{"Fix tidy up.\n\nonly the lint job"}) {
		t.Errorf("first prompt %q", texts)
	}
	if got.Run.Context != "only the lint job" || got.Run.WorkItem != "tidy up" {
		t.Errorf("run did not keep its context and work item: %+v", got.Run)
	}
}

func TestAutomationRunWithAFailingPrecheckIsSkippedAndStartsNoSession(t *testing.T) {
	r := startAutomationRig(t)
	r.runner.fail = true
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "guarded", WorkItem: "tidy up", Repo: "/src/shop"})
	if got.Run.Outcome != domain.AutomationSkipped || got.Run.SessionID != "" || got.Run.Reason == "" {
		t.Fatalf("run = %+v", got.Run)
	}
	if n := liveSessions(t, r.c); n != 0 {
		t.Errorf("%d sessions, want none", n)
	}
	if calls := r.runner.ran(); !reflect.DeepEqual(calls, []string{"/src/shop: sh -c test -f go.mod"}) {
		t.Errorf("precheck calls %q", calls)
	}
}

func TestAutomationRunWithAPassingPrecheckStarts(t *testing.T) {
	r := startAutomationRig(t)
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "guarded", WorkItem: "tidy up", Repo: "/src/shop"})
	if got.Run.Outcome != domain.AutomationSucceeded || got.Run.SessionID == "" {
		t.Fatalf("run = %+v", got.Run)
	}
}

func TestAutomationRunsAreKeptNewestFirstAndStored(t *testing.T) {
	r := startAutomationRig(t)
	first := runAutomation(t, r, rpc.AutomationRunParams{Name: "fix", WorkItem: "one", Repo: "/src/shop"})
	second := runAutomation(t, r, rpc.AutomationRunParams{Name: "fix", WorkItem: "two", Repo: "/src/shop"})
	runAutomation(t, r, rpc.AutomationRunParams{Name: "guarded", WorkItem: "three", Repo: "/src/shop"})
	var got rpc.AutomationRuns
	if err := r.c.Call(context.Background(), rpc.MethodAutomationRuns, rpc.AutomationRunsParams{Name: "fix"}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 2 || got.Runs[0].ID != second.Run.ID || got.Runs[1].ID != first.Run.ID {
		t.Fatalf("runs = %+v", got.Runs)
	}
	waitUntil(t, "runs stored", func() bool { return len(r.store.automationRuns()) == 3 })
}

func TestAutomationRerunRepeatsTheContextAndWorkItem(t *testing.T) {
	r := startAutomationRig(t)
	first := runAutomation(t, r, rpc.AutomationRunParams{Name: "fix", WorkItem: "tidy up", Context: "lint", Repo: "/src/shop"})
	again := runAutomation(t, r, rpc.AutomationRunParams{Rerun: first.Run.ID})
	if again.Run.ID == first.Run.ID || again.Run.Automation != "fix" || again.Run.Context != "lint" || again.Run.WorkItem != "tidy up" {
		t.Fatalf("rerun = %+v", again.Run)
	}
}

func TestAutomationRunReuseSendsToTheLiveSessionOfTheLastRun(t *testing.T) {
	r := startAutomationRig(t)
	first := runAutomation(t, r, rpc.AutomationRunParams{Name: "again", WorkItem: "tidy up", Context: "one", Repo: "/src/shop"})
	second := runAutomation(t, r, rpc.AutomationRunParams{Name: "again", WorkItem: "tidy up", Context: "two", Repo: "/src/shop"})
	if second.Run.SessionID != first.Run.SessionID || second.Run.Outcome != domain.AutomationSucceeded {
		t.Fatalf("second run = %+v, want session %s", second.Run, first.Run.SessionID)
	}
	if n := liveSessions(t, r.c); n != 1 {
		t.Errorf("%d sessions, want 1", n)
	}
	if texts := queuedTexts(t, r.c); !reflect.DeepEqual(texts, []string{"Again one", "Again two"}) {
		t.Errorf("queued %q", texts)
	}
}

func TestAutomationRunOfAnUnknownNameIsNotFound(t *testing.T) {
	r := startAutomationRig(t)
	err := r.c.Call(context.Background(), rpc.MethodAutomationRun, rpc.AutomationRunParams{Name: "nope"}, nil)
	if rpcCode(err) != rpc.CodeNotFound {
		t.Errorf("got %v, want not_found", err)
	}
}

func rpcCode(err error) string {
	var rerr *rpc.Error
	if errors.As(err, &rerr) {
		return rerr.Code
	}
	return ""
}
