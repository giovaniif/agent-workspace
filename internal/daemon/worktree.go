package daemon

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	DefaultWorktreePoll = 10 * time.Second
	DefaultPRPoll       = 60 * time.Second
)

func WithWorktrees(lister app.WorktreeLister, prs app.PRFinder) Option {
	return func(d *Daemon) { d.wt.lister, d.wt.prs = lister, prs }
}

func WithWorktreePoll(every, prEvery time.Duration) Option {
	return func(d *Daemon) { d.wt.every, d.wt.prEvery = every, prEvery }
}

type worktreeScanner struct {
	lister    app.WorktreeLister
	prs       app.PRFinder
	every     time.Duration
	prEvery   time.Duration
	baselined map[string]bool
}

type worktreeHints struct {
	cwd    map[string]string
	claims []domain.WorktreeClaim
	kick   chan struct{}
}

func newWorktreeHints() worktreeHints {
	return worktreeHints{cwd: map[string]string{}, kick: make(chan struct{}, 1)}
}

func (h *worktreeHints) wake() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

type WorktreeRemoved struct{ ID string }

func (e WorktreeRemoved) apply(s *state) rpc.Diff {
	delete(s.worktrees, e.ID)
	s.store.DeleteWorktree(e.ID)
	return rpc.Diff{RemovedWorktree: e.ID}
}

type hookPayload struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command json.RawMessage `json:"command"`
	} `json:"tool_input"`
}

func (p hookPayload) command() string {
	var s string
	if json.Unmarshal(p.ToolInput.Command, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(p.ToolInput.Command, &argv) == nil {
		return strings.Join(argv, " ")
	}
	return ""
}

func (s *state) noteHook(sessionID string, kind domain.HarnessEventKind, payload json.RawMessage, now time.Time) {
	var p hookPayload
	if len(payload) == 0 || json.Unmarshal(payload, &p) != nil {
		return
	}
	if p.Cwd != "" && s.hints.cwd[sessionID] != p.Cwd {
		s.hints.cwd[sessionID] = p.Cwd
		s.hints.wake()
	}
	if kind != domain.EventPostToolUse {
		return
	}
	if cmd := p.command(); domain.IsWorktreeAdd(cmd) {
		kept := s.hints.claims[:0]
		for _, c := range s.hints.claims {
			if now.Sub(c.At) <= domain.ClaimWindow {
				kept = append(kept, c)
			}
		}
		s.hints.claims = append(kept, domain.WorktreeClaim{SessionID: sessionID, Cwd: p.Cwd, Command: cmd, At: now, Tab: s.sessions[sessionID].IsTab()})
		s.hints.wake()
	}
}

func (d *Daemon) watchWorktrees(ctx context.Context) {
	if d.wt.lister == nil {
		return
	}
	d.scanWorktrees(ctx)
	tick, stop := ticker(d.wt.every)
	defer stop()
	var prTimer *time.Timer
	var prFire <-chan time.Time
	if d.wt.prEvery > 0 {
		prTimer = time.NewTimer(d.wt.prEvery)
		prFire = prTimer.C
		defer prTimer.Stop()
	}
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			d.scanWorktrees(ctx)
		case <-d.st.hints.kick:
			d.scanWorktrees(ctx)
		case <-prFire:
			running, err := d.refreshWorktreePRs(ctx)
			if err != nil {
				failures++
			} else {
				failures = 0
			}
			prTimer.Reset(domain.NextPRPoll(d.wt.prEvery, running, failures))
		}
	}
}

func ticker(every time.Duration) (<-chan time.Time, func()) {
	if every <= 0 {
		return nil, func() {}
	}
	t := time.NewTicker(every)
	return t.C, t.Stop
}

func (d *Daemon) scanWorktrees(ctx context.Context) {
	var dirs []string
	if !d.query(func(s *state) {
		for _, ws := range s.workspaces {
			for _, r := range ws.Repos {
				dirs = append(dirs, r.Path)
			}
		}
		for _, cwd := range s.hints.cwd {
			dirs = append(dirs, cwd)
		}
	}) {
		return
	}
	listings := app.ScanWorktrees(ctx, d.wt.lister, dirs)
	adopt := map[string]bool{}
	for _, l := range listings {
		if !d.wt.baselined[l.Main] {
			adopt[l.Main] = true
			d.wt.baselined[l.Main] = true
		}
	}
	now := d.ws.now()
	removedBy := map[string][]string{}
	d.query(func(s *state) {
		known := sorted(s.worktrees)
		hints := s.sessionHints()
		for _, l := range listings {
			attribute := func(w domain.ListedWorktree) string {
				if adopt[l.Main] {
					return ""
				}
				return domain.AttributeWorktree(w, hints, s.hints.claims, now)
			}
			changed, removed := domain.ReconcileWorktrees(known, l, attribute)
			for _, w := range changed {
				s.putWorktree(w)
			}
			for _, id := range removed {
				s.removeWorktree(id)
			}
			removedBy[l.Main] = append(removedBy[l.Main], removed...)
		}
		for _, w := range domain.ReclaimWorktrees(sorted(s.worktrees), s.hints.claims, now) {
			s.putWorktree(w)
		}
	})
	d.dropTurns(ctx, removedBy)
	d.killOrphanPanes(ctx)
}

func (s *state) sessionHints() []domain.SessionHint {
	hints := make([]domain.SessionHint, 0, len(s.hints.cwd))
	for id, cwd := range s.hints.cwd {
		if x, ok := s.sessions[id]; ok {
			hints = append(hints, domain.SessionHint{ID: id, Cwd: cwd, Tab: x.IsTab()})
		}
	}
	sort.Slice(hints, func(i, j int) bool { return hints[i].ID < hints[j].ID })
	return hints
}

func (s *state) putWorktree(w domain.Worktree) {
	prev := s.worktrees[w.ID].SessionID
	w.Ports = s.portsOf(w)
	s.emit(WorktreeChanged{Worktree: w})
	if prev == w.SessionID {
		return
	}
	s.relink(w.ID, prev, w.SessionID)
}

func (s *state) removeWorktree(id string) {
	owner := s.worktrees[id].SessionID
	s.emit(WorktreeRemoved{ID: id})
	s.relink(id, owner, "")
	s.dropTabsOf(id)
}

func (s *state) relink(id, from, to string) {
	if x, ok := s.sessions[from]; ok && from != "" {
		s.emit(SessionChanged{Session: x.DetachWorktree(id)})
	}
	if x, ok := s.sessions[to]; ok && to != "" {
		s.emit(SessionChanged{Session: x.AttachWorktree(id)})
	}
}

func (d *Daemon) refreshWorktreePRs(ctx context.Context) (checksRunning bool, err error) {
	if d.wt.prs == nil {
		return false, nil
	}
	var wts []domain.Worktree
	if !d.query(func(s *state) { wts = sorted(s.worktrees) }) {
		return false, nil
	}
	refresh, err := app.RefreshPRs(ctx, d.wt.prs, wts)
	if err != nil {
		return false, err
	}
	d.query(func(s *state) {
		for _, w := range refresh.Changed {
			cur, ok := s.worktrees[w.ID]
			if !ok {
				continue
			}
			if w.PR != nil && w.PR.State == domain.PRMerged && (cur.PR == nil || cur.PR.State != domain.PRMerged) {
				d.cl.wake()
			}
			cur.PR = w.PR
			s.emit(WorktreeChanged{Worktree: cur})
		}
	})
	return refresh.ChecksRunning, nil
}

func (d *Daemon) worktreeAssign(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.WorktreeAssignParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"id\": string, \"session\": string}"), true
	}
	var msg string
	ok := d.query(func(s *state) {
		w, found := s.worktrees[p.ID]
		if !found {
			msg = "no worktree " + p.ID
			return
		}
		if _, found := s.sessions[p.Session]; p.Session != "" && !found {
			msg = "no session " + p.Session
			return
		}
		w.SessionID = p.Session
		s.putWorktree(w)
	})
	if msg != "" {
		return errorResponse(req.ID, rpc.CodeNotFound, msg), ok
	}
	return result(req.ID, struct{}{}), ok
}
