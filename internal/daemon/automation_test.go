package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const globalAutomations = `
[launcher]
max_parallel = 2

[[automation]]
name = "deps"
agent = "claude"
model = "opus"
prompt = "Update the deps. {{context}}"
schedule = "weekly mon 09:00"

[[automation]]
name = "triage"
agent = "codex"
target = "workspace"
prompt = "Triage {{work_item}}"
enabled = false

[[automation]]
name = "broken"
agent = "gemini"
prompt = "x"
`

func automationRepo(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentws.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func listAutomations(t *testing.T, c *rpc.Client, repo string) rpc.AutomationList {
	t.Helper()
	var got rpc.AutomationList
	if err := c.Call(context.Background(), rpc.MethodAutomationList, rpc.AutomationListParams{Repo: repo}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAutomationListReadsTheGlobalConfigAndReportsInvalidOnes(t *testing.T) {
	c, path := configDaemon(t, globalAutomations)
	got := listAutomations(t, c, "")
	if len(got.Automations) != 2 {
		t.Fatalf("got %+v, want deps and triage", got.Automations)
	}
	deps, triage := got.Automations[0], got.Automations[1]
	if deps.Name != "deps" || deps.Model != "opus" || !deps.Enabled || deps.Source != path || deps.Schedule != "weekly mon 09:00" {
		t.Errorf("deps = %+v", deps)
	}
	if triage.Name != "triage" || triage.Enabled || triage.Target != "workspace" {
		t.Errorf("triage = %+v", triage)
	}
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], `"broken"`) || !strings.Contains(got.Problems[0], path) {
		t.Errorf("problems = %v, want one about broken in %s", got.Problems, path)
	}
}

func TestAutomationListForARepoLetsTheRepoReplaceAGlobalByName(t *testing.T) {
	c, _ := configDaemon(t, globalAutomations)
	repo := automationRepo(t, "[setup]\nrun = [\"make\"]\n\n[[automation]]\nname = \"deps\"\nagent = \"codex\"\nprompt = \"Repo deps\"\n")
	got := listAutomations(t, c, repo)
	if len(got.Automations) != 2 {
		t.Fatalf("got %+v", got.Automations)
	}
	deps := got.Automations[0]
	wantSource := filepath.Join(repo, ".agentws.toml")
	if deps.Name != "deps" || deps.Agent != "codex" || deps.Model != "" || deps.Source != wantSource {
		t.Errorf("deps = %+v, want the repo one from %s", deps, wantSource)
	}
}

func TestAutomationShowFindsOneByName(t *testing.T) {
	c, _ := configDaemon(t, globalAutomations)
	var got rpc.Automation
	if err := c.Call(context.Background(), rpc.MethodAutomationShow, rpc.AutomationShowParams{Name: "triage"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "triage" || got.Agent != "codex" || got.Prompt != "Triage {{work_item}}" {
		t.Errorf("got %+v", got)
	}
}

func TestAutomationShowOfAnUnknownNameIsNotFound(t *testing.T) {
	c, _ := configDaemon(t, globalAutomations)
	err := c.Call(context.Background(), rpc.MethodAutomationShow, rpc.AutomationShowParams{Name: "nope"}, &rpc.Automation{})
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("got %v, want not_found", err)
	}
}

func TestAutomationListReportsAnUnknownKey(t *testing.T) {
	c, _ := configDaemon(t, "[[automation]]\nname = \"x\"\nagent = \"claude\"\nprompt = \"y\"\nshedule = \"daily 09:00\"\n")
	got := listAutomations(t, c, "")
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "shedule") {
		t.Errorf("problems = %v, want one about shedule", got.Problems)
	}
}
