package daemon

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const sendDraftTimeout = 5 * time.Second

func WithHunks(h app.HunkGit) Option {
	return func(d *Daemon) { d.rv.hunks = h }
}

func (s *state) reviewTarget(session string, match func(domain.Worktree) bool) (domain.Worktree, bool) {
	for _, t := range s.reviewTargets(session) {
		if match(t.Worktree) {
			return t.Worktree, true
		}
	}
	return domain.Worktree{}, false
}

func (d *Daemon) sendReview(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ReviewSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "review.send needs a session"), true
	}
	if d.hs.host == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "no terminal host"), true
	}
	var resp *rpc.Response
	ok := d.query(func(s *state) {
		session, found := s.sessions[p.Session]
		if !found {
			resp = errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session)
			return
		}
		draft, open := s.drafts[p.Session]
		if !open || len(draft.Comments) == 0 {
			resp = errorResponse(req.ID, rpc.CodeBadRequest, "the draft has no comments")
			return
		}
		if p.Note != "" {
			draft = draft.WithNote(p.Note)
		}
		draft = draft.Queue()
		s.drafts[p.Session] = draft
		s.putDraft(draft, nil)
		if sent, ok := s.dispatchDraft(session); ok {
			draft = sent
		}
		resp = result(req.ID, draft)
	})
	return resp, ok
}

func (s *state) dispatchDraft(session domain.Session) (domain.ReviewDraft, bool) {
	draft, ok := s.drafts[session.ID]
	if !ok || s.sendDraft == nil || s.pasting[session.ID] || s.sendInFlight(session.ID) {
		return draft, false
	}
	sent, prompt, ok := draft.Dispatch(session, time.Now())
	if !ok {
		return draft, false
	}
	delete(s.drafts, session.ID)
	s.awaiting[session.ID] = sent
	s.pasting[session.ID] = true
	s.emit(DraftChanged{Draft: sent})
	s.sendDraft(session, sent, prompt)
	return sent, true
}

func (d *Daemon) sendDraft(session domain.Session, draft domain.ReviewDraft, prompt string) {
	go func() {
		d.hs.sendMu.Lock()
		defer d.hs.sendMu.Unlock()
		ready := false
		d.query(func(s *state) {
			current, ok := s.sessions[session.ID]
			ready = ok && current.AcceptsSwitch()
		})
		if ready {
			ctx, cancel := context.WithTimeout(context.Background(), sendDraftTimeout)
			err := app.SendPrompt(ctx, d.hs.host, app.PaneID(session.Pane), prompt, app.PasteSettle)
			cancel()
			if err == nil {
				d.query(func(s *state) { s.pasted(draft) })
				return
			}
		}
		d.query(func(s *state) { s.requeueDraft(draft) })
	}()
}

func (s *state) pasted(draft domain.ReviewDraft) {
	delete(s.pasting, draft.Session)
	if current, ok := s.awaiting[draft.Session]; ok && current.ID == draft.ID {
		s.store.PutDraft(current)
	}
	if session, ok := s.sessions[draft.Session]; ok {
		s.dispatchDraft(session)
	}
}

func (s *state) requeueDraft(draft domain.ReviewDraft) {
	delete(s.pasting, draft.Session)
	if s.awaiting[draft.Session].ID == draft.ID {
		delete(s.awaiting, draft.Session)
	}
	back := draft.Unsend()
	if newer, ok := s.drafts[draft.Session]; ok {
		s.store.PutDraft(domain.ReviewDraft{ID: newer.ID, Session: newer.Session, Status: domain.DraftMerged})
		back.Comments = append(back.Comments, newer.Comments...)
	}
	s.drafts[draft.Session] = back
	s.putDraft(back, nil)
}

func (d *Daemon) applyHunk(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.HunkParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" || p.Worktree == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "review.hunk needs a session and a worktree"), true
	}
	if d.rv.hunks == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "hunk staging is not enabled"), true
	}
	if p.Hunk < 0 || p.Hunk >= len(p.File.Hunks) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "no hunk "+strconv.Itoa(p.Hunk)+" in "+p.File.Path), true
	}
	var wt domain.Worktree
	owned := false
	ok := d.query(func(s *state) {
		wt, owned = s.reviewTarget(p.Session, func(w domain.Worktree) bool { return w.ID == p.Worktree })
	})
	if !owned {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session "+p.Session+" does not work in "+p.Worktree), ok
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	if err := app.ApplyHunk(ctx, d.rv.hunks, wt.Path, p.File, p.File.Hunks[p.Hunk], p.Action); err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), ok
	}
	return result(req.ID, struct{}{}), ok
}
