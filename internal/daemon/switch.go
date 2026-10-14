package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const sendSwitchTimeout = 10 * time.Second

func (d *Daemon) switchSession(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.SwitchParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "switch params: "+err.Error()), true
	}
	if (p.Kind != domain.SwitchModel && p.Kind != domain.SwitchEffort) || p.Value == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "switch needs a kind (model or effort) and a value"), true
	}
	if d.hs.host == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "no terminal host"), true
	}
	var resp *rpc.Response
	ok := d.query(func(s *state) {
		session, found := s.sessions[p.SessionID]
		if !found {
			resp = errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.SessionID)
			return
		}
		if !domain.SwitchSupported(session.Harness) {
			resp = errorResponse(req.ID, rpc.CodeBadRequest, "model and effort switching is not supported for "+string(session.Harness))
			return
		}
		next, toSend := session.RequestSwitch(p.Kind, p.Value).Dispatch(time.Now())
		s.emit(SessionChanged{Session: next})
		s.sendSwitches(next, toSend)
		resp = result(req.ID, next)
	})
	return resp, ok
}

func (d *Daemon) deliverable(sessionID string, sws []domain.Switch) []domain.Switch {
	var deliver []domain.Switch
	d.query(func(s *state) {
		current, ok := s.sessions[sessionID]
		if !ok {
			return
		}
		wanted := slices.DeleteFunc(slices.Clone(sws), func(sw domain.Switch) bool { return !slices.Contains(current.Switches, sw) })
		if current.AcceptsSwitch() {
			deliver = wanted
			return
		}
		s.emit(SessionChanged{Session: current.Requeue(wanted)})
	})
	return deliver
}

func (d *Daemon) sendSwitches(session domain.Session, sws []domain.Switch) {
	if len(sws) == 0 {
		return
	}
	go func() {
		d.hs.sendMu.Lock()
		defer d.hs.sendMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), sendSwitchTimeout)
		defer cancel()
		deliver := d.deliverable(session.ID, sws)
		if len(deliver) == 0 {
			return
		}
		err := app.SendSwitches(ctx, d.hs.host, session, deliver, app.PasteSettle)
		if err == nil {
			return
		}
		d.query(func(s *state) {
			if current, ok := s.sessions[session.ID]; ok {
				s.emit(SessionChanged{Session: current.SwitchFailed(deliver)})
			}
		})
	}()
}
