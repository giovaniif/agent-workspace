package daemon

import (
	"strconv"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type MateTurn struct {
	SessionID string
	User      string
	Origin    string
	Asked     []domain.ActionKind
	Hops      int
}

func (e MateTurn) apply(s *state) rpc.Diff {
	session := s.sessions[e.SessionID]
	seat := *session.Mate
	seat.Authority, seat.Origin, seat.Asked, seat.Hops = e.User, e.Origin, e.Asked, e.Hops
	session.Mate = &seat
	s.sessions[session.ID] = session
	s.store.PutSession(session)
	return rpc.Diff{Session: &session}
}

func (s *state) mateSpend(before, after domain.Session, now time.Time) {
	if after.Mate == nil || after.Usage.Tokens <= before.Usage.Tokens {
		return
	}
	name := after.Mate.Name
	s.mateUsage[name] = s.mateUsage[name].AddTokens(after.Usage.Tokens-before.Usage.Tokens, now)
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
	mine := s.sessionApprovals(session.ID)
	decision := domain.Gate(domain.ChainAuthority(*session.Mate, action.Kind), session.Mate.Owner, action, mine, now)
	if decision.Allow {
		if decision.Use >= 0 {
			used := mine[decision.Use]
			used.State = domain.ApprovalUsed
			s.putApproval(used)
		}
		return rpc.HookReply{}
	}
	s.addPendingApproval(session, action, now)
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

func (s *state) findApproval(id string) (domain.Approval, bool) {
	for _, a := range s.approvals {
		if a.ID == id {
			return a, true
		}
	}
	return domain.Approval{}, false
}

func (s *state) putApproval(a domain.Approval) {
	s.store.PutApproval(a)
	for i, x := range s.approvals {
		if x.ID == a.ID {
			s.approvals[i] = a
			return
		}
	}
	s.approvals = append(s.approvals, a)
}

func (s *state) newApprovalID() string {
	for {
		s.approvalSeq++
		id := "approval-" + strconv.Itoa(s.approvalSeq)
		if _, taken := s.findApproval(id); !taken {
			return id
		}
	}
}

func (s *state) addPendingApproval(session domain.Session, action domain.Action, now time.Time) {
	for _, a := range s.approvals {
		if a.SessionID == session.ID && a.State == domain.ApprovalPending && a.Action == action && !a.Expired(now) {
			return
		}
	}
	a := domain.Approval{
		ID: s.newApprovalID(), SessionID: session.ID, Mate: session.Mate.Name, Requester: session.Mate.Authority,
		Action: action, At: now, State: domain.ApprovalPending,
	}
	for _, t := range s.mateThreads {
		if t.SessionID == session.ID {
			a.Channel, a.ThreadTS = t.Channel, t.ThreadTS
			break
		}
	}
	s.putApproval(a)
	if s.pushes != nil {
		s.enqueuePushMessage(domain.ApprovalPush(a))
	}
	if s.approvalAsk != nil {
		s.approvalAsk(a)
	}
}

func (s *state) approvalOwner(a domain.Approval) string {
	if session, ok := s.sessions[a.SessionID]; ok && session.Mate != nil {
		return session.Mate.Owner
	}
	return ""
}
