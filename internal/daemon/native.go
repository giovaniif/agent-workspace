package daemon

import (
	"context"
	"encoding/json"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func (d *Daemon) nativeClient(req rpc.Request) (*rpc.Response, bool) {
	d.clients.mu.Lock()
	h := d.clients.host
	d.clients.mu.Unlock()
	if h == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host"), true
	}
	var p rpc.NativeClientParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Cols < 1 || p.Rows < 1 {
		return errorResponse(req.ID, rpc.CodeBadRequest, "client.native needs {\"cols\",\"rows\"} of at least 1"), true
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	native, err := h.OpenNative(ctx, p.Cols, p.Rows)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
	}
	owners := map[app.PaneID]string{}
	ok := d.query(func(s *state) {
		for _, session := range s.sessions {
			if session.Pane != "" {
				owners[app.PaneID(session.Pane)] = session.ID
			}
		}
	})
	if !ok {
		return nil, false
	}
	out := rpc.NativeClient{Argv: native.Argv, Session: native.Session, Panes: make([]rpc.NativePane, 0, len(native.Panes))}
	for _, np := range native.Panes {
		out.Panes = append(out.Panes, rpc.NativePane{
			Pane:          string(np.Pane),
			Window:        np.Window,
			SessionID:     owners[np.Pane],
			Cols:          np.Cols,
			Rows:          np.Rows,
			MouseAny:      np.MouseAny,
			MouseButton:   np.MouseButton,
			MouseStandard: np.MouseStandard,
			MouseSGR:      np.MouseSGR,
			Alternate:     np.Alternate,
			CursorVisible: np.CursorVisible,
			CursorKeys:    np.CursorKeys,
		})
	}
	return result(req.ID, out), true
}
