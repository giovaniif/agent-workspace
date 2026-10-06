package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type ClientHost interface {
	OpenClient(ctx context.Context, name string, tui app.PaneSpec) (app.Slot, error)
	ClientOpen(ctx context.Context, slot app.Slot) bool
	AttachCommand(slot app.Slot) []string
	FocusSlot(ctx context.Context, slot app.Slot) error
	WidenSidebar(ctx context.Context, slot app.Slot, wide bool) error
	EnsureSlot(ctx context.Context, slot app.Slot) error
	SlotHasPane(ctx context.Context, slot app.Slot) bool
	ShownIn(ctx context.Context, slot app.Slot) app.PaneID
	BelowPane(ctx context.Context, slot app.Slot) app.PaneID
	ShowBelow(ctx context.Context, pane app.PaneID, slot app.Slot) error
	HideBelow(ctx context.Context, slot app.Slot) error
	FocusBelow(ctx context.Context, slot app.Slot) error
	Popup(ctx context.Context, pane app.PaneID) error
	PopupCommand(ctx context.Context, spec app.PaneSpec) error
	Detach(ctx context.Context, slot app.Slot) error
	OpenNative(ctx context.Context, cols, rows int) (app.NativeClient, error)
}

const clientName = "main"

const clientTimeout = 5 * time.Second

type clients struct {
	mu         sync.Mutex
	host       ClientHost
	slot       app.Slot
	dir        string
	watchEvery time.Duration
}

func (d *Daemon) SetClientHost(h ClientHost) {
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	d.clients.host = h
}

func (d *Daemon) dispatchClient(req rpc.Request) *rpc.Response {
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	h := d.clients.host
	if h == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	switch req.Method {
	case rpc.MethodOpenClient:
		var p rpc.OpenClientParams
		if err := json.Unmarshal(req.Params, &p); err != nil || len(p.Command) == 0 {
			return errorResponse(req.ID, rpc.CodeBadRequest, "client.open needs a command")
		}
		d.clients.dir = p.Dir
		d.st.attn.setTerminal(p.Terminal)
		if d.clients.slot == "" || !h.ClientOpen(ctx, d.clients.slot) {
			slot, err := h.OpenClient(ctx, clientName, app.PaneSpec{Name: clientName, Command: p.Command, Env: p.Env})
			if err != nil {
				return errorResponse(req.ID, rpc.CodeFailed, err.Error())
			}
			d.clients.slot = slot
		}
		return result(req.ID, rpc.OpenClient{Slot: string(d.clients.slot), Attach: h.AttachCommand(d.clients.slot)})
	case rpc.MethodClientPopup:
		var p rpc.ClientPopupParams
		if err := json.Unmarshal(req.Params, &p); err != nil || len(p.Command) == 0 {
			return errorResponse(req.ID, rpc.CodeBadRequest, "client.popup needs a command")
		}
		if err := h.PopupCommand(ctx, app.PaneSpec{Command: p.Command, Env: p.Env, Dir: d.clients.dir}); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, struct{}{})
	case rpc.MethodClientDetach:
		if d.clients.slot == "" {
			return errorResponse(req.ID, rpc.CodeNotFound, "no client layout is open")
		}
		if err := h.Detach(ctx, d.clients.slot); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, struct{}{})
	case rpc.MethodClientReview:
		var p rpc.ClientReviewParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "client.review needs {\"open\": bool}")
		}
		if d.clients.slot == "" {
			return errorResponse(req.ID, rpc.CodeFailed, "no client layout is open")
		}
		if err := h.WidenSidebar(ctx, d.clients.slot, p.Open); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, struct{}{})
	default:
		if d.clients.slot == "" {
			return errorResponse(req.ID, rpc.CodeFailed, "no client layout is open")
		}
		if err := h.FocusSlot(ctx, d.clients.slot); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, struct{}{})
	}
}
