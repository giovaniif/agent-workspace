package daemon_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
)

const ciFailed = `
[[automation]]
name = "fix-ci"
agent = "claude"
prompt = "Fix the CI of {{work_item}}."
on = ["pr.ci_failed"]
`

func shopWorktree(checks domain.CheckState) domain.Worktree {
	return domain.Worktree{
		ID: "/src/shop/web-login", Repo: "/src/shop", Path: "/src/shop/web-login", Branch: "feat/login",
		PR: &domain.PullRequest{Number: 42, Title: "Add login", URL: "https://github.com/o/web/pull/42", State: domain.PROpen, Checks: checks},
	}
}

func TestAutomationOnCIFailedStartsOnceWithThePRLink(t *testing.T) {
	r := startScheduleRig(t, ciFailed, time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC), nil)
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckPending)})
	assertNoRunFor(t, r.c, "fix-ci")
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckFailing)})
	waitUntil(t, "an event run", func() bool { return len(automationRunsOf(t, r.c, "fix-ci")) == 1 })
	run := automationRunsOf(t, r.c, "fix-ci")[0]
	if run.Trigger != domain.AutomationTriggerEvent || run.Outcome != domain.AutomationSucceeded || run.Subject != "https://github.com/o/web/pull/42" {
		t.Fatalf("run = %+v", run)
	}
	if texts := queuedTexts(t, r.c); !reflect.DeepEqual(texts, []string{"Fix the CI of https://github.com/o/web/pull/42."}) {
		t.Errorf("first prompt %q", texts)
	}
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckFailing)})
	time.Sleep(60 * time.Millisecond)
	if runs := automationRunsOf(t, r.c, "fix-ci"); len(runs) != 1 {
		t.Errorf("a red CI that stays red ran again: %+v", runs)
	}
}

func TestAutomationOnEventRunsOncePerSubjectAtATime(t *testing.T) {
	r := startScheduleRig(t, ciFailed, time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC), nil)
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckFailing)})
	waitUntil(t, "the first run", func() bool { return len(automationRunsOf(t, r.c, "fix-ci")) == 1 })
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckPending)})
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckFailing)})
	waitUntil(t, "the second event", func() bool { return len(automationRunsOf(t, r.c, "fix-ci")) == 2 })
	skipped := 0
	for _, run := range automationRunsOf(t, r.c, "fix-ci") {
		if run.Outcome == domain.AutomationSkipped && strings.Contains(run.Reason, "live") {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("runs %+v, want one skipped while the first is live", automationRunsOf(t, r.c, "fix-ci"))
	}
	if n := liveSessions(t, r.c); n != 1 {
		t.Errorf("%d sessions, want 1", n)
	}
}

func TestAutomationOnEventIgnoresOtherEventsAndFilters(t *testing.T) {
	config := ciFailed + "\n[automation.filter]\nbranch = \"fix/*\"\n"
	r := startScheduleRig(t, config, time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC), nil)
	r.d.Post(daemon.WorktreeChanged{Worktree: shopWorktree(domain.CheckFailing)})
	assertNoRunFor(t, r.c, "fix-ci")
}
