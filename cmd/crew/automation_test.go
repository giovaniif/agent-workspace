package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestPrintAutomationsListsTriggersAndProblems(t *testing.T) {
	list := rpc.AutomationList{
		Automations: []domain.Automation{
			{Name: "deps", Agent: "claude", Prompt: "x", Schedule: "daily 09:00", Enabled: true, Source: "/h/config.toml"},
			{Name: "fix-ci", Agent: "codex", Target: "workspace", Prompt: "x", On: []string{"pr.ci_failed", "pr.opened"}, Source: "/w/api/.agentws.toml"},
		},
		Problems: []string{"/h/config.toml: automation \"x\": agent must be one of [claude codex omp]"},
	}
	var out bytes.Buffer
	printAutomations(&out, list)
	want := "NAME    AGENT   TARGET        TRIGGER                    ENABLED  SOURCE\n" +
		"deps    claude  new-worktree  daily 09:00                yes      /h/config.toml\n" +
		"fix-ci  codex   workspace     on pr.ci_failed,pr.opened  no       /w/api/.agentws.toml\n" +
		"problem: /h/config.toml: automation \"x\": agent must be one of [claude codex omp]\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPrintAutomationsWithoutAnyTellsWhereToDefineThem(t *testing.T) {
	var out bytes.Buffer
	printAutomations(&out, rpc.AutomationList{})
	if !strings.Contains(out.String(), "[[automation]]") {
		t.Errorf("got %q, want a hint about [[automation]]", out.String())
	}
}

func TestPrintAutomationShowsEveryFieldAndThePrompt(t *testing.T) {
	a := domain.Automation{
		Name: "fix-ci", Agent: "claude", Model: "opus", Effort: "high", Target: "new-worktree",
		On: []string{"pr.ci_failed"}, Precheck: "test -f go.mod", Session: "reuse", Enabled: true,
		Source: "/h/config.toml", Prompt: "Fix the CI of {{work_item}}.\n{{context}}",
	}
	var out bytes.Buffer
	printAutomation(&out, a)
	for _, line := range []string{
		"name:      fix-ci", "agent:     claude", "model:     opus", "effort:    high",
		"target:    new-worktree", "trigger:   on pr.ci_failed", "precheck:  test -f go.mod",
		"session:   reuse", "enabled:   yes", "source:    /h/config.toml",
		"\n  Fix the CI of {{work_item}}.\n  {{context}}\n",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out.String())
		}
	}
}

func TestRunAutomationWithoutASubcommandPrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runAutomation(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: crew automation") {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
	if code := runAutomation([]string{"show"}, &out, &errOut); code != 2 {
		t.Errorf("show without a name: code %d", code)
	}
}

func TestAutomationRunParamsTakeContextFromArgsOrStdin(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  rpc.AutomationRunParams
	}{
		{"no context", []string{"fix"}, "", rpc.AutomationRunParams{Name: "fix"}},
		{"words", []string{"fix", "only", "the", "lint"}, "", rpc.AutomationRunParams{Name: "fix", Context: "only the lint"}},
		{"stdin", []string{"fix", "-"}, "line one\nline two\n", rpc.AutomationRunParams{Name: "fix", Context: "line one\nline two"}},
		{"work item", []string{"--work-item", "#42", "fix", "x"}, "", rpc.AutomationRunParams{Name: "fix", Context: "x", WorkItem: "#42"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := automationRunParams(tc.args, strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatal(err)
			}
			got.Repo = ""
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, err := automationRunParams(nil, strings.NewReader("")); err == nil {
		t.Error("want an error without a name")
	}
}

func TestPrintAutomationRunsShowsOutcomeAndSession(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	runs := []domain.AutomationRun{
		{ID: "r2", Automation: "fix", Trigger: "manual", Start: at.Add(time.Hour), Outcome: "skipped", Reason: "precheck failed", Context: "only lint"},
		{ID: "r1", Automation: "fix", Trigger: "schedule", Start: at, Outcome: "succeeded", SessionID: "s1"},
	}
	var out bytes.Buffer
	printAutomationRuns(&out, runs)
	for _, s := range []string{"r2", "manual", "skipped", "precheck failed", "only lint", "r1", "schedule", "succeeded", "s1"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, out.String())
		}
	}
	if strings.Index(out.String(), "r2") > strings.Index(out.String(), "r1") {
		t.Errorf("runs out of order:\n%s", out.String())
	}
}
