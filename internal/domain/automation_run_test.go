package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestPlanAutomationRun(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)
	base := Automation{Name: "fix-ci", Agent: "claude", Prompt: "Fix {{work_item}} in {{repo}}.", Enabled: true, Source: "/h/config.toml"}
	runs := []AutomationRun{
		{ID: "r1", Automation: "fix-ci", SessionID: "s-old", Start: now.Add(-2 * time.Hour)},
		{ID: "r2", Automation: "fix-ci", SessionID: "s-new", Start: now.Add(-time.Hour)},
		{ID: "r3", Automation: "other", SessionID: "s-other", Start: now.Add(-time.Minute)},
		{ID: "r4", Automation: "fix-ci", Outcome: AutomationSkipped, Start: now.Add(-time.Minute)},
	}
	cases := []struct {
		name string
		edit func(*Automation)
		in   AutomationRunInput
		want AutomationStart
	}{
		{"new worktree in the repo", func(*Automation) {},
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"work item defaults to the name and time", func(*Automation) {},
			AutomationRunInput{Repo: "/w/api", Now: now},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "fix-ci 2026-10-13 09:30", Prompt: "Fix fix-ci 2026-10-13 09:30 in api."}},
		{"context is appended", func(*Automation) {},
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Context: "only lint", Now: now},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "#42", Prompt: "Fix #42 in api.\n\nonly lint"}},
		{"workspace target launches in the repo", func(a *Automation) { a.Target = AutomationTargetWorkspace },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now},
			AutomationStart{Kind: AutomationStartLaunch, Dir: "/w/api", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"worktree target launches in its path", func(a *Automation) { a.Target = "worktree:/w/api-wt" },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now},
			AutomationStart{Kind: AutomationStartLaunch, Dir: "/w/api-wt", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"event fills its variable", func(a *Automation) { a.Prompt = "{{event}}" },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Event: "ci failed", Now: now},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "#42", Prompt: "ci failed"}},
		{"reuse sends to the newest live session", func(a *Automation) { a.Session = AutomationSessionReuse },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now, Runs: runs, Live: map[string]bool{"s-old": true, "s-new": true, "s-other": true}},
			AutomationStart{Kind: AutomationStartSend, Session: "s-new", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"reuse skips ended sessions", func(a *Automation) { a.Session = AutomationSessionReuse },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now, Runs: runs, Live: map[string]bool{"s-old": true}},
			AutomationStart{Kind: AutomationStartSend, Session: "s-old", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"reuse with no live session starts a new one", func(a *Automation) { a.Session = AutomationSessionReuse },
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now, Runs: runs, Live: map[string]bool{"s-other": true}},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "#42", Prompt: "Fix #42 in api."}},
		{"fresh ignores live sessions", func(*Automation) {},
			AutomationRunInput{Repo: "/w/api", WorkItem: "#42", Now: now, Runs: runs, Live: map[string]bool{"s-new": true}},
			AutomationStart{Kind: AutomationStartNew, Workspace: "/w/api", WorkItem: "#42", Prompt: "Fix #42 in api."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.edit(&a)
			got := PlanAutomationRun(a, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestAutomationRunsOfIsNewestFirstAndLimited(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	runs := []AutomationRun{
		{ID: "a", Automation: "x", Start: at},
		{ID: "b", Automation: "y", Start: at.Add(time.Minute)},
		{ID: "c", Automation: "x", Start: at.Add(2 * time.Minute)},
		{ID: "d", Automation: "x", Start: at.Add(time.Minute)},
	}
	ids := func(rs []AutomationRun) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	if got := ids(AutomationRunsOf(runs, "x", 0)); !reflect.DeepEqual(got, []string{"c", "d", "a"}) {
		t.Errorf("all = %v", got)
	}
	if got := ids(AutomationRunsOf(runs, "x", 2)); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Errorf("limited = %v", got)
	}
	if got := ids(AutomationRunsOf(runs, "", 0)); !reflect.DeepEqual(got, []string{"c", "b", "d", "a"}) {
		t.Errorf("every automation = %v", got)
	}
}
