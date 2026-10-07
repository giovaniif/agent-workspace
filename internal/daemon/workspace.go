package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const defaultRefreshInterval = 30 * time.Second

type Option func(*Daemon)

func WithWorkspaces(fs app.WorkspaceFS, git app.RepoInspector) Option {
	return func(d *Daemon) { d.ws.fs, d.ws.git = fs, git }
}

func WithClock(now func() time.Time) Option {
	return func(d *Daemon) { d.ws.now = now }
}

func WithRefreshInterval(every time.Duration) Option {
	return func(d *Daemon) { d.ws.refreshEvery = every }
}

type workspaces struct {
	fs           app.WorkspaceFS
	git          app.RepoInspector
	now          func() time.Time
	refreshEvery time.Duration
	ctx          context.Context
}

type WorkspaceRemoved struct{ Root string }

func (e WorkspaceRemoved) apply(s *state) rpc.Diff {
	delete(s.workspaces, e.Root)
	s.store.DeleteWorkspace(e.Root)
	return rpc.Diff{RemovedWorkspace: e.Root}
}

func (d *Daemon) workspaceMethod(req rpc.Request) (resp *rpc.Response, ok, handled bool) {
	if d.ws.fs == nil {
		return nil, true, false
	}
	switch req.Method {
	case rpc.MethodWorkspaceAdd:
		resp, ok = d.workspaceAdd(req)
	case rpc.MethodWorkspaceRemove:
		resp, ok = d.workspaceRemove(req)
	case rpc.MethodWorkspaceDirs:
		resp, ok = d.workspaceDirs(req), true
	case rpc.MethodProjectAdd:
		resp, ok = d.projectAdd(req)
	case rpc.MethodProjectRemove:
		resp, ok = d.projectRemove(req)
	case rpc.MethodProjectList:
		resp, ok = d.projectList(req)
	default:
		resp, ok = d.workspaceList(req)
	}
	return resp, ok, true
}

func (d *Daemon) workspaceAdd(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.WorkspaceAddParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"path\": string}"), true
	}
	if !filepath.IsAbs(p.Path) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "path must be absolute: "+p.Path), true
	}
	ws, rerr, ok := d.addWorkspace(filepath.Clean(p.Path))
	if rerr != nil {
		return errorResponse(req.ID, rerr.Code, rerr.Message), ok
	}
	return result(req.ID, ws), ok
}

func (d *Daemon) addWorkspace(root string) (domain.Workspace, *rpc.Error, bool) {
	var known []domain.Repo
	if !d.query(func(s *state) { known = s.workspaces[root].Repos }) {
		return domain.Workspace{}, nil, false
	}
	ws, err := app.DiscoverWorkspace(d.ws.fs, root, known)
	if err != nil {
		return domain.Workspace{}, &rpc.Error{Code: rpc.CodeBadRequest, Message: err.Error()}, true
	}
	ws.LastUsed = d.ws.now()
	if !d.commit(WorkspaceChanged{Workspace: ws}) {
		return domain.Workspace{}, nil, false
	}
	go d.refreshWorkspace(root)
	d.st.hints.wake()
	return ws, nil, true
}

func (d *Daemon) workspaceDirs(req rpc.Request) *rpc.Response {
	var p rpc.WorkspaceDirsParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"path\": string}")
	}
	if !filepath.IsAbs(p.Path) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "path must be absolute: "+p.Path)
	}
	children, err := d.ws.fs.Children(filepath.Clean(p.Path))
	if err != nil {
		return errorResponse(req.ID, rpc.CodeNotFound, err.Error())
	}
	if children == nil {
		children = []domain.Child{}
	}
	return result(req.ID, rpc.WorkspaceDirs{Dirs: children})
}

func (d *Daemon) workspaceRemove(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.WorkspaceRemoveParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"root\": string}"), true
	}
	var found bool
	ok := d.query(func(s *state) {
		if _, found = s.workspaces[p.Root]; found {
			d.st.emit(WorkspaceRemoved{Root: p.Root})
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no workspace "+p.Root), ok
	}
	return result(req.ID, struct{}{}), ok
}

func (d *Daemon) workspaceList(req rpc.Request) (*rpc.Response, bool) {
	var list rpc.WorkspaceList
	ok := d.query(func(s *state) {
		list.Workspaces = sorted(s.workspaces)
		if last, found := domain.LastUsedWorkspace(list.Workspaces); found {
			list.LastUsed = last.Root
		}
	})
	if list.Workspaces == nil {
		list.Workspaces = []domain.Workspace{}
	}
	return result(req.ID, list), ok
}

func (d *Daemon) refreshWorkspace(root string) {
	var ws domain.Workspace
	var found bool
	if !d.query(func(s *state) { ws, found = s.workspaces[root] }) || !found {
		return
	}
	fresh := app.RefreshRepoFacts(d.ws.ctx, d.ws.git, ws)
	d.query(func(s *state) {
		cur, found := s.workspaces[root]
		if !found {
			return
		}
		merged := cur
		merged.Repos = domain.MergeRepoState(fresh.Repos, cur.Repos)
		if !reflect.DeepEqual(merged, cur) {
			d.st.emit(WorkspaceChanged{Workspace: merged})
		}
	})
}

func (d *Daemon) refreshWorkspacesEvery(ctx context.Context) {
	if d.ws.fs == nil || d.ws.refreshEvery <= 0 {
		return
	}
	tick := time.NewTicker(d.ws.refreshEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			var roots []string
			d.query(func(s *state) {
				for r := range s.workspaces {
					roots = append(roots, r)
				}
			})
			for _, r := range roots {
				d.refreshWorkspace(r)
			}
		}
	}
}
