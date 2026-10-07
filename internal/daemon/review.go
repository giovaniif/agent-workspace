package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const turnQueue = 64

const reviewTimeout = 10 * time.Second

type turnJob struct {
	session string
	dirs    []string
	sent    *domain.ReviewDraft
}

type review struct {
	git      app.ReviewGit
	hunks    app.HunkGit
	reviewer *app.Reviewer
	jobs     chan turnJob
}

func WithReview(g app.ReviewGit) Option {
	return func(d *Daemon) {
		d.rv = review{git: g, reviewer: app.NewReviewer(g), jobs: make(chan turnJob, turnQueue)}
		d.st.requestTurn = func(session string, dirs []string, sent *domain.ReviewDraft) bool {
			select {
			case d.rv.jobs <- turnJob{session: session, dirs: dirs, sent: sent}:
				return true
			default:
				return false
			}
		}
	}
}

func (d *Daemon) snapshotTurns(ctx context.Context) {
	if d.rv.git == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-d.rv.jobs:
			refs := []string{}
			for _, dir := range job.dirs {
				jctx, cancel := context.WithTimeout(ctx, reviewTimeout)
				if ref, err := app.SnapshotTurn(jctx, d.rv.git, job.session, dir); err == nil {
					refs = append(refs, ref)
				}
				cancel()
			}
			if job.sent != nil {
				linked := job.sent.LinkTurn(refs)
				d.query(func(s *state) { s.store.PutDraft(linked) })
			}
		}
	}
}

func (s *state) reviewTargets(session string) []app.ReviewTarget {
	defaults := map[string]string{}
	for _, ws := range s.workspaces {
		for _, r := range ws.Repos {
			defaults[r.Path] = r.DefaultBranch
		}
	}
	var out []app.ReviewTarget
	if x, ok := s.sessions[session]; ok && x.IsTab() {
		if w, known := s.worktrees[x.Tab]; known {
			return []app.ReviewTarget{{Session: session, Worktree: w, DefaultBranch: defaults[w.Repo]}}
		}
	}
	for _, w := range sorted(s.worktrees) {
		if w.SessionID == session {
			out = append(out, app.ReviewTarget{Session: session, Worktree: w, DefaultBranch: defaults[w.Repo]})
		}
	}
	if cwd := s.hints.cwd[session]; len(out) == 0 && cwd != "" {
		out = append(out, app.ReviewTarget{Session: session, Worktree: domain.Worktree{ID: cwd, Repo: cwd, Path: cwd, SessionID: session}, DefaultBranch: defaults[cwd]})
	}
	return out
}

func (s *state) promptSubmitted(session string) {
	if s.requestTurn == nil {
		return
	}
	var dirs []string
	for _, t := range s.reviewTargets(session) {
		dirs = append(dirs, t.Worktree.Path)
	}
	var sent *domain.ReviewDraft
	if d, ok := s.awaiting[session]; ok {
		delete(s.awaiting, session)
		sent = &d
	}
	accepted := len(dirs) > 0 && s.requestTurn(session, dirs, sent)
	if !accepted && sent != nil {
		s.store.PutDraft(sent.LinkTurn(nil))
	}
}

func (d *Daemon) dispatchReview(req rpc.Request) (*rpc.Response, bool) {
	if d.rv.git == nil {
		return errorResponse(req.ID, rpc.CodeUnknownMethod, "unknown method "+req.Method), true
	}
	switch req.Method {
	case rpc.MethodReviewViewed:
		return d.markViewed(req)
	case rpc.MethodReviewSend:
		return d.sendReview(req)
	case rpc.MethodReviewHunk:
		return d.applyHunk(req)
	}
	var p rpc.ReviewParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "review.open needs a session"), true
	}
	if p.Scope != "" && !slices.Contains(domain.ReviewScopes, p.Scope) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "unknown review scope "+string(p.Scope)), true
	}
	var targets []app.ReviewTarget
	var marks []domain.ViewedMark
	var draft domain.ReviewDraft
	found := false
	ok := d.query(func(s *state) {
		if _, found = s.sessions[p.Session]; !found {
			return
		}
		p.Scope = s.reviewScope(p.Session, p.Scope)
		draft = s.drafts[p.Session]
		ids := map[string]bool{}
		for _, t := range s.reviewTargets(p.Session) {
			if p.Worktree == "" || p.Worktree == t.Worktree.ID {
				targets = append(targets, t)
				ids[t.Worktree.ID] = true
			}
		}
		for _, m := range s.viewed {
			if ids[m.Worktree] {
				marks = append(marks, m)
			}
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session), ok
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].Key() < marks[j].Key() })
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	out := rpc.Review{Scope: p.Scope, Worktrees: d.rv.reviewer.Review(ctx, p.Scope, targets), Viewed: marks, Draft: draft}
	if out.Worktrees == nil {
		out.Worktrees = []domain.WorktreeReview{}
	}
	return result(req.ID, out), ok
}

func (s *state) reviewScope(session string, asked domain.ReviewScope) domain.ReviewScope {
	if asked == "" {
		asked = s.scopes[session]
	}
	if asked == "" {
		asked = domain.ScopeUncommitted
	}
	s.scopes[session] = asked
	return asked
}

func (d *Daemon) markViewed(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ViewedParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Mark.Worktree == "" || p.Mark.Path == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "review.viewed needs a worktree and a path"), true
	}
	ok := d.query(func(s *state) {
		key := p.Mark.Key()
		if p.Viewed {
			s.viewed[key] = p.Mark
			s.store.PutViewed(p.Mark)
			return
		}
		delete(s.viewed, key)
		s.store.DeleteViewed(key)
	})
	return result(req.ID, struct{}{}), ok
}

func (d *Daemon) dropTurns(ctx context.Context, removed map[string][]string) {
	if d.rv.git == nil {
		return
	}
	for main, ids := range removed {
		for _, id := range ids {
			jctx, cancel := context.WithTimeout(ctx, reviewTimeout)
			_ = app.DropTurns(jctx, d.rv.git, main, id)
			cancel()
		}
	}
}
