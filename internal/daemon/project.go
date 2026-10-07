package daemon

import (
	"encoding/json"
	"path/filepath"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type ProjectChanged struct{ Project domain.Project }

func (e ProjectChanged) apply(s *state) rpc.Diff {
	s.projects[e.Project.Root] = e.Project
	s.store.PutProject(e.Project)
	return rpc.Diff{Project: &e.Project}
}

type ProjectRemoved struct{ Root string }

func (e ProjectRemoved) apply(s *state) rpc.Diff {
	delete(s.projects, e.Root)
	s.store.DeleteProject(e.Root)
	return rpc.Diff{RemovedProject: e.Root}
}

func (d *Daemon) projectAdd(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ProjectAddParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"path\": string, \"name\": string, \"setup\": string}"), true
	}
	if !filepath.IsAbs(p.Path) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "path must be absolute: "+p.Path), true
	}
	root := filepath.Clean(p.Path)
	var known []domain.Repo
	if !d.query(func(s *state) { known = s.workspaces[root].Repos }) {
		return nil, false
	}
	discovered, err := app.DiscoverWorkspace(d.ws.fs, root, known)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, err.Error()), true
	}
	project, err := domain.NewProject(discovered, p.Name, p.Setup)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, err.Error()), true
	}
	if _, rerr, ok := d.addWorkspace(root); rerr != nil || !ok {
		if rerr != nil {
			return errorResponse(req.ID, rerr.Code, rerr.Message), ok
		}
		return nil, false
	}
	if !d.commit(ProjectChanged{Project: project}) {
		return nil, false
	}
	return result(req.ID, project), true
}

func (d *Daemon) projectRemove(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ProjectRemoveParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "params must be {\"root\": string}"), true
	}
	var found bool
	ok := d.query(func(s *state) {
		if _, found = s.projects[p.Root]; found {
			d.st.emit(ProjectRemoved{Root: p.Root})
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no project "+p.Root), ok
	}
	return result(req.ID, struct{}{}), ok
}

func (d *Daemon) projectList(req rpc.Request) (*rpc.Response, bool) {
	var list rpc.ProjectList
	ok := d.query(func(s *state) { list.Projects = sorted(s.projects) })
	if list.Projects == nil {
		list.Projects = []domain.Project{}
	}
	return result(req.ID, list), ok
}
