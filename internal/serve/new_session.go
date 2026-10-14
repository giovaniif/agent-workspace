package serve

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func newSession(r *http.Request, d Daemon) (any, error) {
	var p rpc.NewSessionParams
	if err := decodeBody(r, &p); err != nil {
		return nil, err
	}
	if p.Harness != string(domain.HarnessClaude) && p.Harness != string(domain.HarnessCodex) {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: `harness is "claude" or "codex"`}
	}
	if p.Workspace != "" && !filepath.IsAbs(p.Workspace) {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "workspace must be an absolute path"}
	}
	var out domain.Session
	if err := d.Call(r.Context(), rpc.MethodNewSession, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveWorkItem(r *http.Request, d Daemon) (any, error) {
	q := r.URL.Query()
	p := rpc.ResolveWorkItemParams{Workspace: q.Get("workspace"), WorkItem: q.Get("item")}
	if strings.TrimSpace(p.WorkItem) == "" {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "item is required"}
	}
	var out rpc.WorkItemResolved
	if err := d.Call(r.Context(), rpc.MethodSessionResolve, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}
