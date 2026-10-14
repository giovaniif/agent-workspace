package serve_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

func TestServeListsAutomations(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodAutomationList] = rpc.AutomationList{Automations: []domain.Automation{{Name: "fix-ci", Agent: "claude", Prompt: "Fix it.", Enabled: true, Source: "/h/config.toml"}}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/automations", goodToken, "")
	if status != http.StatusOK || !strings.Contains(string(out), `"name":"fix-ci"`) {
		t.Fatalf("status %d %s", status, out)
	}
}

func TestServeRunsAnAutomationWithContext(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodAutomationRun] = rpc.AutomationRunResult{Run: domain.AutomationRun{ID: "r1", Automation: "fix-ci", Outcome: domain.AutomationSucceeded, SessionID: "s1"}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "POST", "/api/v1/automations/fix-ci/run", goodToken, `{"context":"only lint","work_item":"#42"}`)
	if status != http.StatusOK || !strings.Contains(string(out), `"session_id":"s1"`) {
		t.Fatalf("status %d %s", status, out)
	}
	params := f.paramsOf(rpc.MethodAutomationRun)
	if len(params) != 1 || string(params[0]) != `{"name":"fix-ci","context":"only lint","work_item":"#42"}` {
		t.Fatalf("params %s", params)
	}
}

func TestServeAutomationRunRefusesARerunOrRepoFromThePhone(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, _ := do(t, ts, "POST", "/api/v1/automations/fix-ci/run", goodToken, `{"rerun":"r1"}`)
	if status != http.StatusBadRequest {
		t.Errorf("rerun: status %d", status)
	}
	if params := f.paramsOf(rpc.MethodAutomationRun); len(params) != 0 {
		t.Errorf("params %s", params)
	}
}
