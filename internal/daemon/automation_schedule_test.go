package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

type scheduleRig struct {
	automationRig
	clock *testClock
}

func startScheduleRig(t *testing.T, config string, at time.Time, edit func(*memStore)) scheduleRig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	r := scheduleRig{
		automationRig: automationRig{sessionRig: sessionRig{host: &fakeHost{}, wts: &fakeWorktrees{}}, store: &memStore{}, runner: &fakeRunner{}},
		clock:         &testClock{now: at},
	}
	r.store.snap.Workspaces = append(r.store.snap.Workspaces, orchWS)
	if edit != nil {
		edit(r.store)
	}
	r.d, r.path = start(t, r.store,
		daemon.WithHarnesses(r.host, claude.Adapter{}),
		daemon.WithSessions(r.wts, nil, "/h/worktrees"),
		daemon.WithFirstPromptGrace(time.Hour),
		daemon.WithConfig(path),
		daemon.WithAutomationPrecheck(r.runner),
		daemon.WithClock(r.clock.Now),
		daemon.WithAutomationSchedule(5*time.Millisecond),
	)
	r.c = dial(t, r.path)
	return r
}

func automationRunsOf(t *testing.T, c *rpc.Client, name string) []domain.AutomationRun {
	t.Helper()
	var got rpc.AutomationRuns
	if err := c.Call(context.Background(), rpc.MethodAutomationRuns, rpc.AutomationRunsParams{Name: name}, &got); err != nil {
		t.Fatal(err)
	}
	return got.Runs
}

func assertNoRunFor(t *testing.T, c *rpc.Client, name string) {
	t.Helper()
	time.Sleep(60 * time.Millisecond)
	if runs := automationRunsOf(t, c, name); len(runs) != 0 {
		t.Fatalf("runs %+v, want none", runs)
	}
}

const everyMinute = `
[[automation]]
name = "tick"
agent = "claude"
prompt = "Tick."
schedule = "* * * * *"
timezone = "UTC"
`

func TestAutomationScheduleStartsARunWhenItIsDue(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 10, 0, 30, 0, time.UTC)
	r := startScheduleRig(t, everyMinute, t0, nil)
	assertNoRunFor(t, r.c, "tick")
	r.clock.Set(t0.Add(40 * time.Second))
	waitUntil(t, "a scheduled run", func() bool { return len(automationRunsOf(t, r.c, "tick")) == 1 })
	run := automationRunsOf(t, r.c, "tick")[0]
	if run.Trigger != domain.AutomationTriggerSchedule || run.Outcome != domain.AutomationSucceeded || run.SessionID == "" {
		t.Fatalf("run = %+v", run)
	}
	time.Sleep(30 * time.Millisecond)
	if runs := automationRunsOf(t, r.c, "tick"); len(runs) != 1 {
		t.Errorf("ran %d times in one minute", len(runs))
	}
}

func TestAutomationScheduleSkipsWhileTheLastRunIsLive(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 10, 0, 30, 0, time.UTC)
	r := startScheduleRig(t, everyMinute, t0, nil)
	assertNoRunFor(t, r.c, "tick")
	r.clock.Set(t0.Add(40 * time.Second))
	waitUntil(t, "the first run", func() bool { return len(automationRunsOf(t, r.c, "tick")) == 1 })
	r.clock.Set(t0.Add(100 * time.Second))
	waitUntil(t, "the second run", func() bool { return len(automationRunsOf(t, r.c, "tick")) == 2 })
	second := automationRunsOf(t, r.c, "tick")[0]
	if second.Outcome != domain.AutomationSkipped || second.Reason == "" {
		t.Errorf("second run = %+v, want skipped", second)
	}
}

func TestAutomationScheduleRecordsAMissedRunPastTheGrace(t *testing.T) {
	daily := "[[automation]]\nname = \"daily\"\nagent = \"claude\"\nprompt = \"Daily.\"\nschedule = \"daily 09:00\"\ntimezone = \"UTC\"\n"
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	r := startScheduleRig(t, daily, now, func(s *memStore) {
		s.snap.AutomationRuns = []domain.AutomationRun{{
			ID: "old", Automation: "daily", Trigger: domain.AutomationTriggerSchedule, Outcome: domain.AutomationSucceeded,
			Start: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		}}
	})
	waitUntil(t, "a missed run", func() bool { return len(automationRunsOf(t, r.c, "daily")) == 2 })
	missed := automationRunsOf(t, r.c, "daily")[0]
	if missed.Outcome != domain.AutomationMissed || !missed.Start.Equal(time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("run = %+v", missed)
	}
	if n := liveSessions(t, r.c); n != 0 {
		t.Errorf("%d sessions, want none", n)
	}
}

func TestAutomationScheduleRunsAMissedRunInsideTheGrace(t *testing.T) {
	daily := "[[automation]]\nname = \"daily\"\nagent = \"claude\"\nprompt = \"Daily.\"\nschedule = \"daily 09:00\"\ntimezone = \"UTC\"\n"
	now := time.Date(2026, 10, 13, 9, 10, 0, 0, time.UTC)
	r := startScheduleRig(t, daily, now, func(s *memStore) {
		s.snap.AutomationRuns = []domain.AutomationRun{{
			ID: "old", Automation: "daily", Trigger: domain.AutomationTriggerSchedule, Outcome: domain.AutomationSucceeded,
			Start: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		}}
	})
	waitUntil(t, "a late run", func() bool { return len(automationRunsOf(t, r.c, "daily")) == 2 })
	if run := automationRunsOf(t, r.c, "daily")[0]; run.Outcome != domain.AutomationSucceeded {
		t.Fatalf("run = %+v", run)
	}
}

func TestAutomationScheduleOfARepoWaitsForTrust(t *testing.T) {
	repo := t.TempDir()
	body := "[[automation]]\nname = \"repo-tick\"\nagent = \"claude\"\ntarget = \"workspace\"\nprompt = \"Tick.\"\nschedule = \"* * * * *\"\ntimezone = \"UTC\"\n"
	if err := os.WriteFile(filepath.Join(repo, ".agentws.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 13, 10, 0, 30, 0, time.UTC)
	r := startScheduleRig(t, "", t0, func(s *memStore) {
		s.snap.Workspaces = append(s.snap.Workspaces, domain.Workspace{Root: repo, Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "repo", Path: repo}}})
	})
	assertNoRunFor(t, r.c, "repo-tick")
	r.clock.Set(t0.Add(40 * time.Second))
	assertNoRunFor(t, r.c, "repo-tick")
	var list rpc.AutomationList
	if err := r.c.Call(context.Background(), rpc.MethodAutomationList, rpc.AutomationListParams{Repo: repo}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Automations) != 1 || list.Automations[0].Trusted {
		t.Fatalf("list = %+v, want one untrusted automation", list.Automations)
	}
	if err := r.c.Call(context.Background(), rpc.MethodAutomationTrust, rpc.AutomationShowParams{Name: "repo-tick", Repo: repo}, nil); err != nil {
		t.Fatal(err)
	}
	assertNoRunFor(t, r.c, "repo-tick")
	r.clock.Set(t0.Add(100 * time.Second))
	waitUntil(t, "a trusted run", func() bool { return len(automationRunsOf(t, r.c, "repo-tick")) == 1 })
	if run := automationRunsOf(t, r.c, "repo-tick")[0]; run.Repo != repo || run.Outcome != domain.AutomationSucceeded {
		t.Errorf("run = %+v", run)
	}
}
