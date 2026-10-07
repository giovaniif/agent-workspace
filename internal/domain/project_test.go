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

func TestProjectRowsCountTheWorktreesEachProjectHolds(t *testing.T) {
	projects := []Project{
		{Root: "/code/web", Name: "web"},
		{Root: "/code/shop", Name: "Shop"},
		{Root: "/code/api", Name: "api"},
	}
	worktrees := []Worktree{
		{ID: "/h/api/a", Repo: "/code/api", Path: "/h/api/a"},
		{ID: "/h/api/b", Repo: "/code/api", Path: "/h/api/b"},
		{ID: "/code/shop/web/.claude/worktrees/x", Path: "/code/shop/web/.claude/worktrees/x"},
		{ID: "/h/other/c", Repo: "/code/other", Path: "/h/other/c"},
	}
	got := ProjectRows(projects, worktrees)
	want := []ProjectRow{
		{Project: projects[2], Worktrees: 2},
		{Project: projects[1], Worktrees: 1},
		{Project: projects[0], Worktrees: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("rows %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
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
