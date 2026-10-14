package serve_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

func TestServeAddsAWorkspaceFromThePhone(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodWorkspaceAdd] = domain.Workspace{Root: "/home/me/web", Kind: domain.WorkspaceSingle}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "POST", "/api/v1/workspaces", goodToken, `{"path":"~/web"}`)
	if status != http.StatusOK || !strings.Contains(string(out), `"Root":"/home/me/web"`) {
		t.Fatalf("status %d %s", status, out)
	}
	if params := f.paramsOf(rpc.MethodWorkspaceAdd); len(params) != 1 || string(params[0]) != `{"path":"~/web"}` {
		t.Fatalf("params %s", params)
	}
}

func TestServeRemovesAWorkspaceFromThePhone(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "POST", "/api/v1/workspaces/remove", goodToken, `{"root":"/home/me/web"}`)
	if status != http.StatusOK || strings.TrimSpace(string(out)) != "{}" {
		t.Fatalf("status %d %s", status, out)
	}
	if params := f.paramsOf(rpc.MethodWorkspaceRemove); len(params) != 1 || string(params[0]) != `{"root":"/home/me/web"}` {
		t.Fatalf("params %s", params)
	}
}

func TestServeWorkspaceBodiesTakeOnlyTheirOneKey(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, c := range []struct{ path, body string }{
		{"/api/v1/workspaces", `{"path":"/x","setup":"rm -rf /"}`},
		{"/api/v1/workspaces", `{"path":" "}`},
		{"/api/v1/workspaces/remove", `{"root":""}`},
	} {
		if status, out := do(t, ts, "POST", c.path, goodToken, c.body); status != http.StatusBadRequest {
			t.Fatalf("%s %s: status %d %s", c.path, c.body, status, out)
		}
	}
	if len(f.paramsOf(rpc.MethodWorkspaceAdd))+len(f.paramsOf(rpc.MethodWorkspaceRemove)) != 0 {
		t.Fatal("a refused body reached the daemon")
	}
}

func TestServeListsAutomationRuns(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodAutomationRuns] = rpc.AutomationRuns{Runs: []domain.AutomationRun{{ID: "r1", Automation: "fix-ci", Outcome: domain.AutomationSucceeded, SessionID: "s1"}}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/automations/runs?name=fix-ci&limit=5", goodToken, "")
	if status != http.StatusOK || !strings.Contains(string(out), `"id":"r1"`) {
		t.Fatalf("status %d %s", status, out)
	}
	if params := f.paramsOf(rpc.MethodAutomationRuns); len(params) != 1 || string(params[0]) != `{"name":"fix-ci","limit":5}` {
		t.Fatalf("params %s", params)
	}
	if status, _ := do(t, ts, "GET", "/api/v1/automations/runs?limit=many", goodToken, ""); status != http.StatusBadRequest {
		t.Fatalf("bad limit: status %d", status)
	}
}
