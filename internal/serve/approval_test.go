package serve_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

func TestServeListsApprovals(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodApprovalList] = []domain.Approval{{ID: "a1", Mate: "helper", State: domain.ApprovalPending}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/approvals", goodToken, "")
	if status != http.StatusOK || !strings.Contains(string(out), `"ID":"a1"`) || !strings.Contains(string(out), `"Mate":"helper"`) {
		t.Fatalf("status %d %s", status, out)
	}
}

func TestServeListsNoApprovalsAsAnEmptyList(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/approvals", goodToken, "")
	if status != http.StatusOK || strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("status %d %s", status, out)
	}
}

func TestServeDecidesAnApproval(t *testing.T) {
	for _, c := range []struct {
		verb, params string
	}{
		{"approve", `{"id":"a1","approve":true}`},
		{"deny", `{"id":"a1","approve":false}`},
	} {
		f := newFakeDaemon()
		f.devices[goodToken] = phone
		f.results[rpc.MethodApprovalDecide] = domain.Approval{ID: "a1", State: domain.ApprovalGranted}
		_, ts := startServer(t, f, serve.Config{URL: publicURL})
		status, out := do(t, ts, "POST", "/api/v1/approvals/a1/"+c.verb, goodToken, "")
		if status != http.StatusOK || !strings.Contains(string(out), `"ID":"a1"`) {
			t.Fatalf("%s: status %d %s", c.verb, status, out)
		}
		params := f.paramsOf(rpc.MethodApprovalDecide)
		if len(params) != 1 || string(params[0]) != c.params {
			t.Fatalf("%s: params %s", c.verb, params)
		}
	}
}

func TestServeApprovalDecideNeedsAToken(t *testing.T) {
	f := newFakeDaemon()
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	if status, _ := do(t, ts, "POST", "/api/v1/approvals/a1/approve", "", ""); status != http.StatusUnauthorized {
		t.Fatalf("status %d", status)
	}
	if params := f.paramsOf(rpc.MethodApprovalDecide); len(params) != 0 {
		t.Fatalf("params %s", params)
	}
}
