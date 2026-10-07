package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type ShellTabChanged struct{ Tab domain.ShellTab }

func (e ShellTabChanged) apply(s *state) rpc.Diff {
	s.shellTabs[e.Tab.ID] = e.Tab
	return rpc.Diff{ShellTab: &e.Tab}
}

type ShellTabRemoved struct{ ID string }

func (e ShellTabRemoved) apply(s *state) rpc.Diff {
	delete(s.shellTabs, e.ID)
	return rpc.Diff{RemovedShellTab: e.ID}
}

type ActiveTabChanged struct{ Worktree, Tab string }

func (e ActiveTabChanged) apply(s *state) rpc.Diff {
	s.activeTabs[e.Worktree] = e.Tab
	return rpc.Diff{ActiveTab: &rpc.ActiveTab{Worktree: e.Worktree, Tab: e.Tab}}
}

type tabInput struct {
	session  domain.Session
	found    bool
	home     domain.Worktree
	inHome   bool
	tabs     []domain.Tab
	active   string
	sessions map[string]domain.Session
}

func (s *state) tabInput(id string) tabInput {
	in := tabInput{sessions: map[string]domain.Session{}}
	in.session, in.found = s.sessions[id]
	if !in.found {
		return in
	}
	home, ok := domain.TabHome(in.session, sorted(s.worktrees), sorted(s.projects))
	if !ok {
		return in
	}
	in.home, in.inHome = s.worktrees[home], true
	in.tabs = domain.WorktreeTabs(home, sorted(s.sessions), sorted(s.shellTabs))
	in.active = s.activeTabs[home]
	for _, t := range in.tabs {
		if t.Kind == domain.TabAgent {
			in.sessions[t.ID] = s.sessions[t.ID]
		}
	}
	return in
}

func (s *state) noteActiveTab(session domain.Session) {
	home, ok := domain.TabHome(session, sorted(s.worktrees), sorted(s.projects))
	if ok && s.activeTabs[home] != session.ID {
		s.emit(ActiveTabChanged{Worktree: home, Tab: session.ID})
	}
}

func (d *Daemon) tabMethod(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.TabParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" params: "+err.Error()), true
	}
	if d.hs.host == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no terminal host"), true
	}
	var in tabInput
	if !d.query(func(s *state) { in = s.tabInput(p.Session) }) {
		return nil, false
	}
	switch {
	case !in.found:
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session), true
	case !in.inHome:
		return errorResponse(req.ID, rpc.CodeBadRequest, "session "+p.Session+" is not in a project worktree; tabs are for project worktrees"), true
	}
	if shown := d.shownPane(); shown != "" {
		for _, t := range in.tabs {
			if t.Pane == string(shown) {
				in.active = t.ID
			}
		}
	}
	var tab domain.Tab
	var rerr *rpc.Error
	var ok bool
	switch req.Method {
	case rpc.MethodTabNew:
		tab, rerr, ok = d.newTab(in, p)
	case rpc.MethodTabShow:
		tab, rerr, ok = d.showTabByID(in, p.Tab)
	case rpc.MethodTabStep:
		next, found := domain.StepTab(in.tabs, in.active, p.Delta)
		if !found {
			return errorResponse(req.ID, rpc.CodeNotFound, "no tabs"), true
		}
		tab, rerr, ok = d.showTabByID(in, next.ID)
	default:
		tab, rerr, ok = d.closeTab(in, p.Tab)
	}
	if rerr != nil {
		return errorResponse(req.ID, rerr.Code, rerr.Message), ok
	}
	return result(req.ID, tab), ok
}

func (d *Daemon) shownPane() app.PaneID {
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	if d.clients.host == nil || d.clients.slot == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	return d.clients.host.ShownIn(ctx, d.clients.slot)
}

func (d *Daemon) newTab(in tabInput, p rpc.TabParams) (domain.Tab, *rpc.Error, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), terminalTimeout)
	defer cancel()
	now := d.ws.now()
	switch p.Kind {
	case string(domain.TabShell):
		id := newID()
		pane, err := d.hs.host.Create(ctx, app.PaneSpec{Name: "tab-" + id, Dir: in.home.Path, Env: d.paneEnv(in.session.ID)})
		if err != nil {
			return domain.Tab{}, &rpc.Error{Code: rpc.CodeLaunchFailed, Message: err.Error()}, true
		}
		sh := domain.ShellTab{ID: id, Worktree: in.home.ID, Pane: string(pane), At: now}
		if !d.commit(ShellTabChanged{Tab: sh}) {
			return domain.Tab{}, nil, false
		}
		tab := domain.Tab{ID: id, Kind: domain.TabShell, Label: "shell", Pane: sh.Pane}
		return d.showTab(in.home.ID, tab, true)
	case string(domain.TabAgent):
		adapter, found := d.hs.adapters[domain.Harness(p.Harness)]
		if !found {
			return domain.Tab{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: "no harness " + p.Harness}, true
		}
		var task domain.Task
		if !d.query(func(s *state) { task = s.tasks[in.session.TaskID] }) {
			return domain.Tab{}, nil, false
		}
		spec := adapter.Launch(app.LaunchRequest{Name: domain.NameFor(task, nil), Dir: in.home.Path, Model: p.Model, Effort: p.Effort})
		pane, err := d.hs.host.Create(ctx, spec)
		if err != nil {
			return domain.Tab{}, &rpc.Error{Code: rpc.CodeLaunchFailed, Message: err.Error()}, true
		}
		session := domain.Session{
			ID: newID(), TaskID: in.session.TaskID, Harness: adapter.Harness(), Pane: string(pane),
			Model: p.Model, Effort: p.Effort, State: domain.StateIdle, Dir: in.home.Path, Tab: in.home.ID,
			StartedAt: now, StartModel: p.Model, StartEffort: p.Effort, StartWorkspace: in.session.StartWorkspace,
		}
		if !d.commit(SessionChanged{Session: session}) {
			_ = d.hs.host.Kill(ctx, pane)
			return domain.Tab{}, nil, false
		}
		tab := domain.Tab{ID: session.ID, Kind: domain.TabAgent, Label: string(session.Harness), State: session.State, Pane: session.Pane}
		return d.showTab(in.home.ID, tab, true)
	}
	return domain.Tab{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: "kind must be shell or agent, not " + p.Kind}, true
}

func (d *Daemon) showTabByID(in tabInput, id string) (domain.Tab, *rpc.Error, bool) {
	for _, t := range in.tabs {
		if t.ID == id {
			return d.showTab(in.home.ID, t, false)
		}
	}
	return domain.Tab{}, &rpc.Error{Code: rpc.CodeNotFound, Message: "no tab " + id}, true
}

func (d *Daemon) showTab(worktree string, tab domain.Tab, focus bool) (domain.Tab, *rpc.Error, bool) {
	d.clients.mu.Lock()
	err := d.placeTabLocked(app.PaneID(tab.Pane), focus)
	d.clients.mu.Unlock()
	if err != nil && !errors.Is(err, errNoLayout) {
		return tab, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}, true
	}
	ok := d.query(func(s *state) {
		if cur, found := s.sessions[tab.ID]; found && tab.Kind == domain.TabAgent {
			s.focus(cur)
		}
		if s.activeTabs[worktree] != tab.ID {
			s.emit(ActiveTabChanged{Worktree: worktree, Tab: tab.ID})
		}
	})
	return tab, nil, ok
}

func (d *Daemon) placeTabLocked(pane app.PaneID, focus bool) error {
	if focus {
		return d.showInMainLocked(pane)
	}
	if d.clients.host == nil || d.clients.slot == "" {
		return errNoLayout
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	return d.hs.host.Show(ctx, pane, d.clients.slot)
}

func (d *Daemon) closeTab(in tabInput, id string) (domain.Tab, *rpc.Error, bool) {
	if id == "" {
		id = in.active
	}
	if id == "" && in.session.IsTab() {
		id = in.session.ID
	}
	var closing domain.Tab
	found := false
	for _, t := range in.tabs {
		if t.ID == id {
			closing, found = t, true
		}
	}
	if !found {
		return domain.Tab{}, &rpc.Error{Code: rpc.CodeNotFound, Message: "no tab " + id}, true
	}
	after, hasAfter := domain.TabAfterClose(in.tabs, closing.ID, in.active)
	if closing.Kind == domain.TabAgent && !hasAfter {
		if _, ok, err := d.endAndRefill(in.sessions[closing.ID]); err != nil || !ok {
			if err != nil {
				return closing, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}, true
			}
			return closing, nil, false
		}
		return closing, nil, true
	}
	if hasAfter && after.ID != in.active {
		if _, rerr, ok := d.showTab(in.home.ID, after, false); rerr != nil || !ok {
			return closing, rerr, ok
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	if closing.Kind == domain.TabAgent {
		ended, err := d.sessions().End(ctx, in.sessions[closing.ID])
		if err != nil {
			return closing, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}, true
		}
		return closing, nil, d.query(func(s *state) {
			if cur, ok := s.sessions[ended.ID]; ok {
				s.emit(SessionChanged{Session: cur.End()})
			}
		})
	}
	if err := d.hs.host.Kill(ctx, app.PaneID(closing.Pane)); err != nil {
		return closing, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}, true
	}
	if !hasAfter {
		d.clients.mu.Lock()
		if d.clients.host != nil && d.clients.slot != "" {
			_ = d.clients.host.EnsureSlot(ctx, d.clients.slot)
		}
		d.clients.mu.Unlock()
	}
	return closing, nil, d.commit(ShellTabRemoved{ID: closing.ID})
}
