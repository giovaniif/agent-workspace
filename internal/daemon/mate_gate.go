package daemon

import (
	"slices"
	"strconv"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type MateTurn struct {
	SessionID string
	User      string
}

func (e MateTurn) apply(s *state) rpc.Diff {
	session := s.sessions[e.SessionID]
	seat := *session.Mate
	seat.Authority = e.User
	session.Mate = &seat
	s.sessions[session.ID] = session
	s.store.PutSession(session)
	return rpc.Diff{Session: &session}
}

func (s *state) gateHook(h rpc.Hook, now time.Time) rpc.HookReply {
	if kind, ok := hookEvent(h); !ok || kind != domain.EventPreToolUse || h.Action == nil {
		return rpc.HookReply{}
	}
	session, ok := domain.SessionOnPane(s.sessionList(), h.Pane)
	if !ok || session.Mate == nil {
		return rpc.HookReply{}
	}
	action := *h.Action
	decision := domain.Gate(session.Mate.Authority, session.Mate.Owner, action, s.sessionApprovals(session.ID), now)
	if decision.Allow {
		if decision.Use >= 0 {
			used := s.sessionApprovals(session.ID)[decision.Use].ID
			s.approvals = slices.DeleteFunc(s.approvals, func(a domain.Approval) bool { return a.ID == used })
		}
		return rpc.HookReply{}
	}
	s.addPendingApproval(session.ID, action, now)
	return rpc.HookReply{Output: domain.GateDenyOutput(action)}
}

func (s *state) sessionApprovals(sessionID string) []domain.Approval {
	var out []domain.Approval
	for _, a := range s.approvals {
		if a.SessionID == sessionID {
			out = append(out, a)
		}
	}
	return out
}

func (s *state) addPendingApproval(sessionID string, action domain.Action, now time.Time) {
	for _, a := range s.approvals {
		if a.SessionID == sessionID && a.State == domain.ApprovalPending && a.Action == action && !now.After(a.At.Add(domain.ApprovalTTL)) {
			return
		}
	}
	s.approvalSeq++
	s.approvals = append(s.approvals, domain.Approval{
		ID: "approval-" + strconv.Itoa(s.approvalSeq), SessionID: sessionID, Action: action, At: now, State: domain.ApprovalPending,
	})
}

func (s *state) pendingApprovals() []domain.Approval {
	out := []domain.Approval{}
	for _, a := range s.approvals {
		if a.State == domain.ApprovalPending {
			out = append(out, a)
		}
	}
	return out
}
