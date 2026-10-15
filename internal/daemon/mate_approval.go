package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const approvalSlackTimeout = 30 * time.Second

func (d *Daemon) approvalDecide(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ApprovalDecideParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.ID) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "approval.decide needs an id"), true
	}
	a, rerr, ok := d.decideApproval(p.ID, "", true, p.Approve)
	if !ok {
		return nil, false
	}
	if rerr != nil {
		return &rpc.Response{V: rpc.Version, ID: req.ID, Error: rerr}, true
	}
	return result(req.ID, a), true
}

func (d *Daemon) decideApproval(id, by string, local, allow bool) (domain.Approval, *rpc.Error, bool) {
	var out domain.Approval
	var rerr *rpc.Error
	if !d.query(func(s *state) {
		a, found := s.findApproval(id)
		if !found {
			rerr = &rpc.Error{Code: rpc.CodeNotFound, Message: "no approval " + id}
			return
		}
		owner := s.approvalOwner(a)
		if local {
			if owner == "" {
				owner = "local"
			}
			by = owner
		}
		next, err := domain.DecideApproval(a, by, owner, allow, d.ws.now())
		switch {
		case errors.Is(err, domain.ErrApprovalNotOwner):
			rerr = &rpc.Error{Code: rpc.CodeUnauthorized, Message: err.Error()}
			return
		case err != nil:
			rerr = &rpc.Error{Code: rpc.CodeStale, Message: err.Error()}
			return
		}
		s.putApproval(next)
		out = next
	}) {
		return out, nil, false
	}
	if rerr == nil {
		go d.afterApproval(out)
	}
	return out, rerr, true
}

func (d *Daemon) afterApproval(a domain.Approval) {
	resp, ok := d.sessionSend(rpc.Request{Method: rpc.MethodSessionSend, Params: mustJSON(rpc.SessionSendParams{Session: a.SessionID, Text: domain.ApprovalSessionPrompt(a)})})
	if ok && resp != nil && resp.Error != nil {
		log.Printf("[mates] approval %s: the session did not get the decision: %s", a.ID, resp.Error.Message)
	}
	if a.Message == "" || a.Channel == "" {
		return
	}
	client := d.mateClient(a.Mate)
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ws.ctx, approvalSlackTimeout)
	defer cancel()
	if err := client.UpdateMessage(ctx, a.Channel, a.Message, domain.ApprovalOutcomeText(a)); err != nil {
		log.Printf("[mates] %s: %v", a.Mate, err)
	}
}

func (d *Daemon) askApproval(a domain.Approval) {
	if a.Channel == "" {
		return
	}
	client := d.mateClient(a.Mate)
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ws.ctx, approvalSlackTimeout)
	defer cancel()
	ts, err := client.PostApproval(ctx, a.Channel, a.ThreadTS, domain.ApprovalRequestText(a), a.ID)
	if err != nil {
		log.Printf("[mates] %s: %v", a.Mate, err)
		return
	}
	d.query(func(s *state) {
		if cur, ok := s.findApproval(a.ID); ok {
			cur.Message = ts
			s.putApproval(cur)
		}
	})
}

func (d *Daemon) slackApproval(name string, e app.SlackEvent) {
	var allow bool
	switch e.ActionID {
	case domain.ApprovalApproveAction:
		allow = true
	case domain.ApprovalDenyAction:
	default:
		return
	}
	var mate string
	if !d.query(func(s *state) {
		if a, ok := s.findApproval(e.Value); ok {
			mate = a.Mate
		}
	}) || mate != name {
		return
	}
	if _, rerr, _ := d.decideApproval(e.Value, e.User, false, allow); rerr != nil && rerr.Code != rpc.CodeUnauthorized {
		log.Printf("[mates] %s: approval %s: %s", name, e.Value, rerr.Message)
	}
}
