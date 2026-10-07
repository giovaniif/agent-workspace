package domain

import (
	"fmt"
	"path/filepath"
	"sort"
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

type ProjectRow struct {
	Project   Project
	Worktrees int
}

func ProjectOfWorktree(projects []Project, w Worktree) (Project, bool) {
	if p, found := ProjectOf(projects, w.Repo); found {
		return p, true
	}
	return ProjectOf(projects, w.Path)
}

func ProjectRows(projects []Project, worktrees []Worktree) []ProjectRow {
	held := map[string]int{}
	for _, w := range worktrees {
		if p, found := ProjectOfWorktree(projects, w); found {
			held[p.Root]++
		}
	}
	rows := make([]ProjectRow, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, ProjectRow{Project: p, Worktrees: held[p.Root]})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := strings.ToLower(rows[i].Project.Name), strings.ToLower(rows[j].Project.Name)
		if a != b {
			return a < b
		}
		return rows[i].Project.Root < rows[j].Project.Root
	})
	return rows
}
