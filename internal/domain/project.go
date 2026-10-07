package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Project struct {
	Root  string
	Name  string
	Setup string
}

func NewProject(ws Workspace, name, setup string) (Project, error) {
	if len(ws.Repos) == 0 {
		return Project{}, fmt.Errorf("%s has no git repos", ws.Root)
	}
	p := Project{Root: ws.Root, Name: strings.TrimSpace(name), Setup: strings.TrimSpace(setup)}
	if p.Name == "" {
		p.Name = filepath.Base(ws.Root)
	}
	return p, nil
}

func ProjectOf(projects []Project, path string) (Project, bool) {
	if path == "" {
		return Project{}, false
	}
	path = filepath.Clean(path)
	var best Project
	found := false
	for _, p := range projects {
		root := filepath.Clean(p.Root)
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			continue
		}
		if !found || len(root) > len(filepath.Clean(best.Root)) {
			best, found = p, true
		}
	}
	return best, found
}
