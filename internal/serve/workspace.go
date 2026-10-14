package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func decodeOne(r *http.Request, into any, what string) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return &rpc.Error{Code: rpc.CodeBadRequest, Message: "the body takes only " + what + ": " + err.Error()}
	}
	return nil
}

func addWorkspace(r *http.Request, d Daemon) (any, error) {
	var p rpc.WorkspaceAddParams
	if err := decodeOne(r, &p, "path"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "path is empty"}
	}
	var out domain.Workspace
	if err := d.Call(r.Context(), rpc.MethodWorkspaceAdd, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func removeWorkspace(r *http.Request, d Daemon) (any, error) {
	var p rpc.WorkspaceRemoveParams
	if err := decodeOne(r, &p, "root"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Root) == "" {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "root is empty"}
	}
	if err := d.Call(r.Context(), rpc.MethodWorkspaceRemove, p, nil); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}
