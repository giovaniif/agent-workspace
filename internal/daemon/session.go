package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type sessionDeps struct {
	worktrees    app.WorktreeAdder
	setup        app.SetupFunc
	worktreeHome string
	scripts      app.CommandRunner
	addMu        sync.Mutex
	resumeMu     sync.Mutex

	firstPromptGrace time.Duration
}

const defaultFirstPromptGrace = 15 * time.Second

func WithFirstPromptGrace(grace time.Duration) Option {
	return func(d *Daemon) { d.sess.firstPromptGrace = grace }
}

func WithSessions(worktrees app.WorktreeAdder, setup app.SetupFunc, worktreeHome string) Option {
	return func(d *Daemon) {
		d.sess = sessionDeps{worktrees: worktrees, setup: setup, worktreeHome: worktreeHome, scripts: d.sess.scripts, firstPromptGrace: d.sess.firstPromptGrace}
	}
}

func WithProjectSetup(runner app.CommandRunner) Option {
	return func(d *Daemon) { d.sess.scripts = runner }
}

func (d *Daemon) sessions() app.Sessions {
	return app.Sessions{Host: d.hs.host, Worktrees: serialAdder{mu: &d.sess.addMu, inner: d.sess.worktrees}, Setup: d.sess.setup, Runner: d.sess.scripts}
}

type serialAdder struct {
	mu    *sync.Mutex
	inner app.WorktreeAdder
}

func (a serialAdder) AddWorktree(ctx context.Context, repo, path, branch, base string) (app.AddedWorktree, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inner.AddWorktree(ctx, repo, path, branch, base)
}

type newSessionInput struct {
	ws      domain.Workspace
	task    domain.Task
	isNew   bool
	taken   []string
	missing string

	projectSetup string
}

func (d *Daemon) newSession(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.NewSessionParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.new params: "+err.Error()), true
	}
	session, rerr, ok := d.startSession(p, "")
	if !ok {
		return nil, false
	}
	if rerr != nil {
		return errorResponse(req.ID, rerr.Code, rerr.Message), true
	}
	return result(req.ID, session), true
}

func (d *Daemon) startSession(p rpc.NewSessionParams, prompt string) (domain.Session, *rpc.Error, bool) {
	adapter, ok := d.hs.adapters[domain.Harness(p.Harness)]
	if !ok || d.hs.host == nil || d.sess.worktrees == nil {
		return domain.Session{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: "no harness " + p.Harness}, true
	}
	if p.Workspace != "" {
		if !filepath.IsAbs(p.Workspace) {
			return domain.Session{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: "workspace must be an absolute path: " + p.Workspace}, true
		}
		p.Workspace = filepath.Clean(p.Workspace)
	}
	if p.Workspace != "" && d.ws.fs != nil {
		var known bool
		if !d.query(func(s *state) { _, known = s.workspaces[p.Workspace] }) {
			return domain.Session{}, nil, false
		}
		if !known {
			if _, rerr, ok := d.addWorkspace(p.Workspace); rerr != nil || !ok {
				return domain.Session{}, rerr, ok
			}
		}
	}
	parsed := domain.ParseWorkItem(p.WorkItem)
	var in newSessionInput
	if !d.query(func(s *state) { in = s.newSessionInput(p.Workspace, parsed) }) {
		return domain.Session{}, nil, false
	}
	switch {
	case in.missing != "" && p.Workspace == "":
		return domain.Session{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: in.missing}, true
	case in.missing != "":
		return domain.Session{}, &rpc.Error{Code: rpc.CodeNotFound, Message: in.missing}, true
	}
	if in.isNew {
		in.task.ID = newID()
	}
	slug := domain.TaskSlug(in.task)
	plan := domain.PlanSessionStart(in.ws, slug, d.sess.worktreeHome, in.taken)
	name := slug
	if plan.Worktree != nil {
		name = plan.Worktree.Branch
	}
	started, err := d.sessions().Start(d.ws.ctx, app.NewSession{
		ID: newID(), Task: in.task, Plan: plan, Harness: adapter,
		Name: name, Model: p.Model, Effort: p.Effort, Prompt: prompt,
		ProjectSetup: in.projectSetup,
	})
	if err != nil {
		if started.Worktree != nil && !d.query(func(s *state) { s.putWorktree(*started.Worktree) }) {
			return domain.Session{}, nil, false
		}
		return domain.Session{}, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}, true
	}
	started.Session.StartedAt = d.ws.now()
	started.Session.StartModel = started.Session.Model
	started.Session.StartEffort = started.Session.Effort
	started.Session.StartWorkspace = in.ws.Root
	queued := false
	ok = d.query(func(s *state) {
		if in.isNew {
			s.emit(TaskChanged{Task: in.task})
			d.resolveTitleAsync(in.task)
		}
		if started.Worktree != nil {
			s.emit(WorktreeChanged{Worktree: *started.Worktree})
		}
		s.emit(SessionChanged{Session: started.Session})
		if domain.SendableText(p.Prompt) {
			s.queueFirstPrompt(started.Session, p.Prompt)
			queued = true
		}
		if ws, found := s.workspaces[in.ws.Root]; found {
			ws.LastUsed = d.ws.now()
			s.emit(WorkspaceChanged{Workspace: ws})
		}
	})
	if queued {
		d.releaseFirstPromptAfterGrace(started.Session.ID)
	}
	return started.Session, nil, ok
}

func (s *state) newSessionInput(root string, parsed domain.Task) newSessionInput {
	var in newSessionInput
	if root == "" {
		ws, found := domain.LastUsedWorkspace(sorted(s.workspaces))
		if !found {
			in.missing = "no workspace to start in; add one with `agentws workspace add`"
			return in
		}
		in.ws = ws
	} else {
		ws, found := s.workspaces[root]
		if !found {
			in.missing = "no workspace " + root
			return in
		}
		in.ws = ws
	}
	if project, found := domain.ProjectOf(sorted(s.projects), in.ws.Root); found {
		in.projectSetup = project.Setup
	}
	known, found := domain.FindTask(sorted(s.tasks), parsed)
	in.task, in.isNew = known, !found
	if !found {
		in.task = parsed
	}
	for _, w := range s.worktrees {
		in.taken = append(in.taken, w.Path)
	}
	return in
}

func (d *Daemon) sessionByRef(req rpc.Request) (domain.Session, *rpc.Response, bool) {
	var ref rpc.SessionRef
	if err := json.Unmarshal(req.Params, &ref); err != nil {
		return domain.Session{}, errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" params: "+err.Error()), true
	}
	var session domain.Session
	var found bool
	if !d.query(func(s *state) { session, found = s.sessions[ref.ID] }) {
		return domain.Session{}, nil, false
	}
	if !found {
		return domain.Session{}, errorResponse(req.ID, rpc.CodeNotFound, "no session "+ref.ID), true
	}
	if d.hs.host == nil {
		return domain.Session{}, errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host"), true
	}
	return session, nil, true
}

func (d *Daemon) endSession(req rpc.Request) (*rpc.Response, bool) {
	session, resp, ok := d.sessionByRef(req)
	if resp != nil || !ok {
		return resp, ok
	}
	ended, ok, err := d.endAndRefill(session)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
	}
	return result(req.ID, ended), ok
}

func (d *Daemon) resumeSession(req rpc.Request) (*rpc.Response, bool) {
	d.sess.resumeMu.Lock()
	defer d.sess.resumeMu.Unlock()
	session, resp, ok := d.sessionByRef(req)
	if resp != nil || !ok {
		return resp, ok
	}
	adapter, found := d.hs.adapters[session.Harness]
	if !found {
		return errorResponse(req.ID, rpc.CodeBadRequest, "no harness "+string(session.Harness)), true
	}
	var task domain.Task
	if !d.query(func(s *state) { task = s.tasks[session.TaskID] }) {
		return nil, false
	}
	resumed, err := d.sessions().Resume(d.ws.ctx, session, adapter, domain.NameFor(task, nil))
	switch {
	case errors.Is(err, app.ErrNotResumable):
		return errorResponse(req.ID, rpc.CodeBadRequest, "session "+session.ID+" is live or has no harness session to resume"), true
	case err != nil:
		return errorResponse(req.ID, rpc.CodeLaunchFailed, err.Error()), true
	}
	if !d.commit(SessionChanged{Session: resumed}) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.hs.host.Kill(ctx, app.PaneID(resumed.Pane))
		return nil, false
	}
	return result(req.ID, resumed), true
}

func (d *Daemon) endAndRefill(session domain.Session) (domain.Session, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	ended, err := d.sessions().End(ctx, session)
	if err != nil {
		return domain.Session{}, true, err
	}
	var next domain.Session
	var hasNext, wasInView bool
	ok := d.query(func(s *state) {
		if cur, found := s.sessions[ended.ID]; found {
			wasInView = cur.Focused
			if wasInView {
				next, hasNext = domain.NextInView(sorted(s.tasks), sorted(s.sessions), ended.ID)
			}
			ended = cur.End()
			s.emit(SessionChanged{Session: ended})
		}
	})
	if ok && wasInView {
		d.refillMain(next, hasNext)
	}
	return ended, ok, nil
}

func (d *Daemon) refillMain(next domain.Session, hasNext bool) {
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	if d.clients.host == nil || d.clients.slot == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	if hasNext && d.hs.host.Show(ctx, app.PaneID(next.Pane), d.clients.slot) == nil {
		d.query(func(s *state) {
			if cur, found := s.sessions[next.ID]; found {
				s.focus(cur)
			}
		})
		return
	}
	_ = d.clients.host.EnsureSlot(ctx, d.clients.slot)
}

var errNoLayout = errors.New("no client layout is open")

func (d *Daemon) focusSession(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.SessionFocusParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "session.focus params: "+err.Error()), true
	}
	var session domain.Session
	var found bool
	if !d.query(func(s *state) { session, found = s.sessions[p.ID] }) {
		return nil, false
	}
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.ID), true
	}
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	if session.Pane != "" && d.hs.host != nil {
		if err := d.showInMainLocked(app.PaneID(session.Pane)); err != nil && !errors.Is(err, errNoLayout) {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
		}
	}
	ok := d.query(func(s *state) {
		if cur, found := s.sessions[p.ID]; found {
			s.focus(cur)
		}
	})
	return result(req.ID, struct{}{}), ok
}

func (d *Daemon) showInMainLocked(pane app.PaneID) error {
	if d.clients.host == nil || d.clients.slot == "" {
		return errNoLayout
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	if err := d.hs.host.Show(ctx, pane, d.clients.slot); err != nil {
		return err
	}
	return d.clients.host.FocusSlot(ctx, d.clients.slot)
}

func (d *Daemon) reconcilePanes(ctx context.Context) {
	var onPanes []domain.Session
	for _, s := range d.restored {
		if s.Pane != "" {
			onPanes = append(onPanes, s)
		}
	}
	if d.hs.host == nil || len(onPanes) == 0 {
		return
	}
	ended, err := app.ReconcilePanes(ctx, d.hs.host, onPanes)
	if err != nil {
		return
	}
	panes := map[string]string{}
	for _, s := range onPanes {
		panes[s.ID] = s.Pane
	}
	var next domain.Session
	var hasNext, wasInView bool
	ok := d.query(func(s *state) {
		dead := map[string]bool{}
		for _, e := range ended {
			if cur, found := s.sessions[e.ID]; found && cur.Pane == panes[e.ID] {
				dead[cur.ID] = true
			}
		}
		for id := range dead {
			if s.sessions[id].Focused {
				wasInView = true
				next, hasNext = domain.NextInView(sorted(s.tasks), aliveOr(sorted(s.sessions), dead, id), id)
			}
		}
		for id := range dead {
			s.emit(SessionChanged{Session: s.sessions[id].End()})
		}
	})
	if ok && wasInView {
		d.refillMain(next, hasNext)
	}
}

func aliveOr(sessions []domain.Session, dead map[string]bool, keep string) []domain.Session {
	var out []domain.Session
	for _, s := range sessions {
		if !dead[s.ID] || s.ID == keep {
			out = append(out, s)
		}
	}
	return out
}
