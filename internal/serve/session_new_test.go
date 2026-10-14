package serve_test

import (
	"net/http"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

func TestServeNewSessionStartsASessionWithItsPrompt(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodNewSession] = domain.Session{ID: "s9", TaskID: "t9", Harness: domain.HarnessCodex, State: domain.StateRunning}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	body := `{"workspace":"/home/me/api","work_item":"api#12","harness":"codex","model":"gpt-5.5","effort":"high","prompt":"start with the tests"}`
	status, out := do(t, ts, "POST", "/api/v1/sessions", goodToken, body)
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, out)
	}
	golden(t, "session-new.json", out)
	want := `{"workspace":"/home/me/api","work_item":"api#12","harness":"codex","model":"gpt-5.5","effort":"high","prompt":"start with the tests"}`
	params := f.paramsOf(rpc.MethodNewSession)
	if len(params) != 1 || string(params[0]) != want {
		t.Fatalf("params %s", params)
	}
}

func TestServeNewSessionRefusesBadBodies(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, body := range []string{`nope`, `{}`, `{"work_item":"x"}`, `{"work_item":"x","harness":"claude","workspace":"api"}`} {
		status, out := do(t, ts, "POST", "/api/v1/sessions", goodToken, body)
		if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
			t.Fatalf("%s: status %d %s", body, status, out)
		}
	}
	if got := f.paramsOf(rpc.MethodNewSession); len(got) != 0 {
		t.Fatalf("a bad body reached the daemon: %s", got)
	}
}

func TestServeNewSessionReportsALaunchFailureWithItsOutput(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodNewSession] = &rpc.Error{Code: rpc.CodeFailed, Message: "setup /w/api: npm ci: exit status 1\nnpm ERR! missing lockfile"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "POST", "/api/v1/sessions", goodToken, `{"work_item":"x","harness":"claude"}`)
	if status != http.StatusInternalServerError || errorCode(t, out) != rpc.CodeFailed {
		t.Fatalf("status %d %s", status, out)
	}
	golden(t, "session-new-failed.json", out)
}

func TestServeResolvesAWorkItem(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodSessionResolve] = rpc.WorkItemResolved{Source: "pr", Ref: "api#12", Title: "Fix the login redirect", Worktree: "api-12", Workspace: "/home/me/api"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/work-items/resolve?workspace=%2Fhome%2Fme%2Fapi&item=api%2312", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, out)
	}
	golden(t, "work-item-resolved.json", out)
	want := `{"workspace":"/home/me/api","work_item":"api#12"}`
	params := f.paramsOf(rpc.MethodSessionResolve)
	if len(params) != 1 || string(params[0]) != want {
		t.Fatalf("params %s", params)
	}
}

func TestServeResolveRequiresAnItemAndPassesTheDaemonsVerdictThrough(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodSessionResolve] = &rpc.Error{Code: rpc.CodeBadRequest, Message: "unsupported link: use a GitHub pull request URL"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/work-items/resolve", goodToken, "")
	if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
		t.Fatalf("status %d %s", status, out)
	}
	if got := f.paramsOf(rpc.MethodSessionResolve); len(got) != 0 {
		t.Fatalf("an empty item reached the daemon: %s", got)
	}
	status, out = do(t, ts, "GET", "/api/v1/work-items/resolve?item=https%3A%2F%2Fexample.com", goodToken, "")
	if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
		t.Fatalf("status %d %s", status, out)
	}
	golden(t, "work-item-bad.json", out)
}

func TestServeNewSessionStartsWithoutAWorkItem(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "POST", "/api/v1/sessions", goodToken, `{"harness":"claude"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, out)
	}
	if got := f.paramsOf(rpc.MethodNewSession); len(got) != 1 {
		t.Fatalf("calls %s", got)
	}
}
