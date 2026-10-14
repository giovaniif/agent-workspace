package tui_test

import (
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestAutomationKeyPicksAnAutomationAndRunsItWithContext(t *testing.T) {
	m, c := namingModel(t, domain.Task{ID: "t1", Text: "tidy"})
	c.autos = []domain.Automation{
		{Name: "deps", Agent: "claude", Enabled: true},
		{Name: "fix-ci", Agent: "codex", Enabled: true},
		{Name: "paused", Agent: "claude"},
	}
	m = pressCmd(m, key("a"))
	out := screen(m)
	if !strings.Contains(out, "AUTOMATION") || !strings.Contains(out, "deps") {
		t.Fatalf("a should open the automation picker on the first automation:\n%s", out)
	}
	m = press(m, "j")
	if out := screen(m); !strings.Contains(out, "fix-ci") {
		t.Fatalf("j should move to the next automation:\n%s", out)
	}
	m = press(m, "j")
	if out := screen(m); !strings.Contains(out, "deps") || strings.Contains(out, "paused") {
		t.Fatalf("the picker wraps and skips paused automations:\n%s", out)
	}
	m = press(m, "k")
	m = update(m, keyEnter)
	if out := screen(m); !strings.Contains(out, "RUN fix-ci") {
		t.Fatalf("enter should ask for the context:\n%s", out)
	}
	m = typeText(m, "only the lint job")
	m = pressCmd(m, keyEnter)
	last := c.calls[len(c.calls)-1]
	if last.method != rpc.MethodAutomationRun || last.params != (rpc.AutomationRunParams{Name: "fix-ci", Context: "only the lint job"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	if out := screen(m); strings.Contains(out, "RUN fix-ci") {
		t.Errorf("the prompt should close after the run:\n%s", out)
	}
}

func TestAutomationKeyWithoutAutomationsSaysWhereToAddThem(t *testing.T) {
	m, _ := namingModel(t, domain.Task{ID: "t1", Text: "tidy"})
	m = pressCmd(m, key("a"))
	if out := screen(m); !strings.Contains(out, "no automations") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestAutomationPromptEscCancels(t *testing.T) {
	m, c := namingModel(t, domain.Task{ID: "t1", Text: "tidy"})
	c.autos = []domain.Automation{{Name: "deps", Agent: "claude", Enabled: true}}
	m = pressCmd(m, key("a"))
	m = update(m, keyEnter)
	m = update(m, keyEsc)
	if out := screen(m); strings.Contains(out, "RUN deps") || strings.Contains(out, "AUTOMATION") {
		t.Fatalf("esc should close the prompt:\n%s", out)
	}
	for _, call := range c.calls {
		if call.method == rpc.MethodAutomationRun {
			t.Errorf("esc ran the automation")
		}
	}
}

func TestAutomationHelpListsTheKey(t *testing.T) {
	m, _ := namingModel(t, domain.Task{ID: "t1", Text: "tidy"})
	if out := screen(press(m, "?")); !strings.Contains(out, "run an automation") {
		t.Errorf("help:\n%s", out)
	}
}
