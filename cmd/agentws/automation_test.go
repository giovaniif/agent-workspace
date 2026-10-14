package main

import (
	"bytes"
	"strings"
	"testing"

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
	if code := runAutomation(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: agentws automation") {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
	if code := runAutomation([]string{"show"}, &out, &errOut); code != 2 {
		t.Errorf("show without a name: code %d", code)
	}
}
