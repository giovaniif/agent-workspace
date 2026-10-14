package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const interruptTimeout = 5 * time.Second

type SendsChanged struct{ Sends []domain.QueuedSend }

func (e SendsChanged) apply(s *state) rpc.Diff {
	s.sends = e.Sends
	sends := slices.Clone(e.Sends)
	if sends == nil {
		sends = []domain.QueuedSend{}
	}
	return rpc.Diff{Sends: &sends}
}

type sendFlight struct {
	send   domain.QueuedSend
	pasted bool
}

func (d *Daemon) sessionInput(req rpc.Request) (*rpc.Response, bool) {
	switch req.Method {
	case rpc.MethodSessionSend:
		return d.sessionSend(req)
	case rpc.MethodSessionUnsend:
		return d.sessionUnsend(req)
	default:
		return d.sessionInterrupt(req)
	}
}

func newSendID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "send-" + hex.EncodeToString(b)
}

func (d *Daemon) sessionSend(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.SessionSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.send needs a session and text"), true
	}
	if !domain.SendableText(p.Text) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.send needs non-empty text"), true
	}
	if d.hs.host == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host"), true
	}
	q := domain.QueuedSend{ID: newSendID(), Session: p.Session, Text: p.Text, QueuedAt: time.Now()}
	var resp *rpc.Response
	ok := d.query(func(s *state) {
		session, found := s.sessions[p.Session]
		switch {
		case !found:
			resp = errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session)
			return
		case session.Ended:
			resp = errorResponse(req.ID, rpc.CodeFailed, "session "+p.Session+" has ended")
			return
		}
		queue := append(slices.Clone(s.sends), q)
		next, dispatch := domain.NextSend(session, queue, s.sendBusy(session.ID))
		if dispatch {
			queue, _ = domain.Unsend(queue, next.Session, next.ID)
		}
		if !dispatch || next.ID != q.ID {
			s.emit(SendsChanged{Sends: queue})
		}
		if dispatch {
			s.startSend(session, next)
		}
		resp = result(req.ID, rpc.SessionSent{ID: q.ID, Queued: !dispatch || next.ID != q.ID})
	})
	return resp, ok
}

func (d *Daemon) sessionUnsend(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.SessionUnsendParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" || p.ID == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.unsend needs a session and an id"), true
	}
	var resp *rpc.Response
	ok := d.query(func(s *state) {
		next, found := domain.Unsend(s.sends, p.Session, p.ID)
		if !found {
			resp = errorResponse(req.ID, rpc.CodeNotFound, "no queued send "+p.ID+" for session "+p.Session)
			return
		}
		s.emit(SendsChanged{Sends: next})
		resp = result(req.ID, struct{}{})
	})
	return resp, ok
}

func (d *Daemon) sessionInterrupt(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.SessionTarget
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.interrupt needs a session"), true
	}
	var session domain.Session
	var found bool
	if !d.query(func(s *state) { session, found = s.sessions[p.Session] }) {
		return nil, false
	}
	switch {
	case !found:
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session), true
	case session.Ended:
		return errorResponse(req.ID, rpc.CodeFailed, "session "+p.Session+" has ended"), true
	case d.hs.host == nil:
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host"), true
	}
	ctx, cancel := context.WithTimeout(context.Background(), interruptTimeout)
	defer cancel()
	if err := d.hs.host.SendKeys(ctx, app.PaneID(session.Pane), "Escape"); err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
	}
	return result(req.ID, struct{}{}), true
}

func (s *state) sendInFlight(session string) bool {
	_, ok := s.inFlight[session]
	return ok
}

func (s *state) sendBusy(session string) bool {
	return s.pasting[session] || s.booting[session] || s.sendInFlight(session)
}

func (s *state) settleSend(session string) {
	if f, ok := s.inFlight[session]; ok && f.pasted {
		delete(s.inFlight, session)
	}
}

func (s *state) dispatchSend(session domain.Session) {
	next, ok := domain.NextSend(session, s.sends, s.sendBusy(session.ID))
	if !ok {
		return
	}
	queue, _ := domain.Unsend(s.sends, next.Session, next.ID)
	s.emit(SendsChanged{Sends: queue})
	s.startSend(session, next)
}

func (s *state) startSend(session domain.Session, q domain.QueuedSend) {
	s.inFlight[session.ID] = sendFlight{send: q}
	s.pasteSend(session, q)
}

func (s *state) dropSends(session string) {
	delete(s.inFlight, session)
	delete(s.booting, session)
	if next, changed := domain.DropSends(s.sends, session); changed {
		s.emit(SendsChanged{Sends: next})
	}
}

func (d *Daemon) pasteSend(session domain.Session, q domain.QueuedSend) {
	go func() {
		d.hs.sendMu.Lock()
		defer d.hs.sendMu.Unlock()
		ready := false
		d.query(func(s *state) {
			current, ok := s.sessions[session.ID]
			ready = ok && !current.Ended && current.AcceptsSwitch()
		})
		if ready {
			ctx, cancel := context.WithTimeout(context.Background(), sendDraftTimeout)
			err := app.SendPrompt(ctx, d.hs.host, app.PaneID(session.Pane), q.Text, app.PasteSettle)
			cancel()
			if err == nil {
				d.query(func(s *state) { s.sendPasted(q) })
				return
			}
		}
		d.query(func(s *state) { s.sendFailed(q) })
	}()
}

func (s *state) sendPasted(q domain.QueuedSend) {
	if f, ok := s.inFlight[q.Session]; ok && f.send.ID == q.ID {
		s.inFlight[q.Session] = sendFlight{send: q, pasted: true}
	}
}

func (s *state) sendFailed(q domain.QueuedSend) {
	f, ok := s.inFlight[q.Session]
	if !ok || f.send.ID != q.ID {
		return
	}
	delete(s.inFlight, q.Session)
	if session, found := s.sessions[q.Session]; found && !session.Ended {
		s.emit(SendsChanged{Sends: domain.RequeueSend(s.sends, q)})
		s.dispatchDraft(session)
	}
}

func (s *state) queueFirstPrompt(session domain.Session, text string) {
	q := domain.QueuedSend{ID: newSendID(), Session: session.ID, Text: text, QueuedAt: time.Now()}
	s.booting[session.ID] = true
	s.emit(SendsChanged{Sends: append(slices.Clone(s.sends), q)})
}

func (d *Daemon) releaseFirstPromptAfterGrace(sessionID string) {
	grace := d.sess.firstPromptGrace
	if grace <= 0 {
		grace = defaultFirstPromptGrace
	}
	time.AfterFunc(grace, func() {
		d.query(func(s *state) {
			if !s.booting[sessionID] {
				return
			}
			delete(s.booting, sessionID)
			if session, ok := s.sessions[sessionID]; ok {
				s.dispatchSend(session)
			}
		})
	})
}
