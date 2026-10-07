package domain

import "testing"

func TestNewProjectNamesItAfterItsFolderAndTrimsTheScript(t *testing.T) {
	ws := Workspace{Root: "/code/shop", Kind: WorkspaceOrchestration, Repos: []Repo{{Name: "api", Path: "/code/shop/api"}}}
	p, err := NewProject(ws, "  ", "  make env  \n")
	if err != nil {
		t.Fatal(err)
	}
	if p != (Project{Root: "/code/shop", Name: "shop", Setup: "make env"}) {
		t.Errorf("project = %+v", p)
	}
	named, err := NewProject(ws, " Shop ", "")
	if err != nil || named.Name != "Shop" || named.Setup != "" {
		t.Errorf("named = %+v, %v", named, err)
	}
}

func TestNewProjectRejectsAFolderWithoutRepos(t *testing.T) {
	ws := Workspace{Root: "/code/empty", Kind: WorkspaceOrchestration}
	if _, err := NewProject(ws, "", ""); err == nil {
		t.Error("a folder with no git repos became a project")
	}
}

func TestProjectOfPicksTheProjectHoldingAPath(t *testing.T) {
	projects := []Project{
		{Root: "/code/shop", Name: "shop"},
		{Root: "/code/shop/api", Name: "api"},
		{Root: "/code/solo", Name: "solo"},
	}
	cases := []struct {
		path string
		want string
	}{
		{"/code/shop/web", "shop"},
		{"/code/shop", "shop"},
		{"/code/shop/api/.claude/worktrees/x", "api"},
		{"/code/shop/api", "api"},
		{"/code/solo/", "solo"},
		{"/code/shopping", ""},
		{"/code", ""},
		{"", ""},
	}
	for _, c := range cases {
		got, found := ProjectOf(projects, c.path)
		if found != (c.want != "") || got.Name != c.want {
			t.Errorf("ProjectOf(%q) = %+v, %v; want %q", c.path, got, found, c.want)
		}
	}
}
