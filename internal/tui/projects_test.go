package tui_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

func withProjects(st rpc.State) rpc.State {
	st = withWorkspaces(st)
	st.Projects = []domain.Project{
		{Root: "/src/shop", Name: "shop", Setup: "make env"},
		{Root: "/src/api", Name: "api"},
	}
	st.Worktrees = append(st.Worktrees, domain.Worktree{ID: "/h/api/old", Repo: "/src/api", Path: "/h/api/old", Branch: "old"})
	return st
}

func projectsModel(t *testing.T, st rpc.State, opts tui.Options) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	opts.Theme, opts.Now, opts.Calls = tui.Latte(), clock, c
	m := tui.New(opts)
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(st)), c
}

func TestProjectsSectionSitsAboveTheSessionsWithItsWorktrees(t *testing.T) {
	m, _ := projectsModel(t, withProjects(fixture(1, 0)), tui.Options{})
	out := screen(m)
	projects, sessions := strings.Index(out, "PROJECTS"), strings.Index(out, "SESSIONS")
	if projects < 0 || sessions < 0 || projects > sessions {
		t.Fatalf("no projects section above the sessions:\n%s", out)
	}
	section := out[projects:sessions]
	for _, want := range []string{"api", "1 worktree", "shop"} {
		if !strings.Contains(section, want) {
			t.Fatalf("projects section lacks %q:\n%s", want, section)
		}
	}
	if strings.Index(section, "api") > strings.Index(section, "shop") {
		t.Fatalf("projects not sorted by name:\n%s", section)
	}
}

func TestNoProjectsSectionWithoutProjects(t *testing.T) {
	m, _ := projectsModel(t, withWorkspaces(fixture(1, 0)), tui.Options{})
	if out := screen(m); strings.Contains(out, "PROJECTS") {
		t.Fatalf("an empty projects section is drawn:\n%s", out)
	}
}

func TestProjectDiffsAddAndRemoveRows(t *testing.T) {
	m, _ := projectsModel(t, withWorkspaces(fixture(1, 0)), tui.Options{})
	m = update(m, tui.DiffMsg(rpc.Diff{Project: &domain.Project{Root: "/src/web", Name: "web"}}))
	if out := screen(m); !strings.Contains(out, "PROJECTS") || !strings.Contains(out, "web") {
		t.Fatalf("added project not shown:\n%s", out)
	}
	m = update(m, tui.DiffMsg(rpc.Diff{RemovedProject: "/src/web"}))
	if out := screen(m); strings.Contains(out, "PROJECTS") {
		t.Fatalf("removed project still shown:\n%s", out)
	}
}

func TestProjectsPanelRegistersAProjectWithItsSetupScript(t *testing.T) {
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	m = press(m, "P", "a")
	m = typeText(m, "/src/web")
	m = pressCmd(m, keyTab)
	m = typeText(m, "cp ~/.env.web .env")
	m = pressCmd(m, keyEnter)
	if got := c.methods(); !slices.Equal(got, []string{rpc.MethodProjectAdd}) {
		t.Fatalf("calls %v", got)
	}
	want := rpc.ProjectAddParams{Path: "/src/web", Setup: "cp ~/.env.web .env"}
	if !reflect.DeepEqual(c.calls[0].params, want) {
		t.Fatalf("params %+v, want %+v", c.calls[0].params, want)
	}
	if out := screen(m); strings.Contains(out, "path ") {
		t.Fatalf("the add form stayed open after the add:\n%s", out)
	}
}

func TestProjectsPanelShowsWhyAnAddFailed(t *testing.T) {
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	c.err = &rpc.Error{Code: rpc.CodeBadRequest, Message: "/tmp has no git repos"}
	m = press(m, "P", "a")
	m = typeText(m, "/tmp")
	m = pressCmd(m, keyEnter)
	if out := screen(m); !strings.Contains(out, "no git repos") {
		t.Fatalf("the refusal is not shown:\n%s", out)
	}
}

func TestProjectsPanelRemovesTheChosenProjectAfterYes(t *testing.T) {
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	m = press(m, "P", "j", "d")
	if out := screen(m); !strings.Contains(out, "remove project shop? y/n") {
		t.Fatalf("no confirmation:\n%s", out)
	}
	pressCmd(m, key("y"))
	if got := c.methods(); !slices.Equal(got, []string{rpc.MethodProjectRemove}) {
		t.Fatalf("calls %v", got)
	}
	if !reflect.DeepEqual(c.calls[0].params, rpc.ProjectRemoveParams{Root: "/src/shop"}) {
		t.Fatalf("params %+v", c.calls[0].params)
	}
}

func TestProjectsPanelRemoveAnsweredNoKeepsTheProject(t *testing.T) {
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	m = press(m, "P", "d")
	pressCmd(m, key("n"))
	if len(c.methods()) != 0 {
		t.Fatalf("calls %v", c.methods())
	}
}

func TestOpeningAProjectStartsTheNewSessionDialogInIt(t *testing.T) {
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	m = press(m, "P")
	m = pressCmd(m, keyEnter)
	if out := screen(m); !strings.Contains(out, "New session") || !strings.Contains(out, "‹ api ›") {
		t.Fatalf("the dialog is not open in the project:\n%s", out)
	}
	m = typeText(m, "fix login")
	pressCmd(m, keyEnter)
	if len(c.calls) == 0 || c.calls[0].params.(rpc.NewSessionParams).Workspace != "/src/api" {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestClickingAProjectRowOpensTheDialogInIt(t *testing.T) {
	m, _ := projectsModel(t, withProjects(fixture(1, 0)), tui.Options{})
	m = clickOn(t, m, "shop")
	if out := screen(m); !strings.Contains(out, "New session") || !strings.Contains(out, "‹ shop ›") {
		t.Fatalf("clicking the project did not open the dialog in it:\n%s", out)
	}
}

func TestOpeningAProjectWithAPopupPassesItsRoot(t *testing.T) {
	popup := rpc.ClientPopupParams{Command: []string{"agentws", "tui", "--new-session"}, Env: map[string]string{"AGENTWS_HOME": "/h"}}
	m, c := projectsModel(t, withProjects(rpc.State{}), tui.Options{DialogPopup: popup})
	m = press(m, "P", "j")
	pressCmd(m, keyEnter)
	if got := c.methods(); !slices.Equal(got, []string{rpc.MethodClientPopup}) {
		t.Fatalf("calls %v", got)
	}
	got := c.calls[0].params.(rpc.ClientPopupParams)
	if got.Env["AGENTWS_PROJECT"] != "/src/shop" || got.Env["AGENTWS_HOME"] != "/h" {
		t.Fatalf("popup env %+v", got.Env)
	}
	if _, leaked := popup.Env["AGENTWS_PROJECT"]; leaked {
		t.Fatal("opening a project changed the shared popup env")
	}
}

func manyProjects(n int, root string) rpc.State {
	var st rpc.State
	for i := range n {
		name := fmt.Sprintf("proj%02d", i)
		st.Projects = append(st.Projects, domain.Project{Root: root + name, Name: name})
	}
	return st
}

func TestALateAddAnswerLeavesANewerAddFormAlone(t *testing.T) {
	m, _ := projectsModel(t, withProjects(rpc.State{}), tui.Options{})
	m = press(m, "P", "a")
	m = typeText(m, "/src/first")
	next, first := m.Update(keyEnter)
	m = next.(tui.Model)
	m = pressCmd(m, keyEsc)
	m = press(m, "a")
	m = typeText(m, "/src/second")
	m = run(m, first)
	if out := screen(m); !strings.Contains(out, "/src/second") {
		t.Fatalf("the first add's answer closed the second form:\n%s", out)
	}
}

func TestProjectsPanelKeepsTheNameOfAProjectWithALongRoot(t *testing.T) {
	m, _ := projectsModel(t, manyProjects(1, "/home/someone/work/clients/acme/monorepo/services/"), tui.Options{})
	if out := screen(press(m, "P")); !strings.Contains(out, "proj00") {
		t.Fatalf("the long root hid the project's name:\n%s", out)
	}
}

func TestManyProjectsFitASmallSidebar(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 16})
	m = update(m, tui.StateMsg(manyProjects(30, "/p/")))
	if lines := strings.Split(screen(m), "\n"); len(lines) > 16 {
		t.Fatalf("%d lines in a 16-row sidebar", len(lines))
	}
	if out := screen(m); !strings.Contains(out, "more") {
		t.Fatalf("hidden projects are not counted:\n%s", out)
	}
}

func TestProjectsPanelScrollsWithItsCursor(t *testing.T) {
	m, _ := projectsModel(t, manyProjects(60, "/p/"), tui.Options{})
	m = press(m, "P")
	for range 55 {
		m = press(m, "j")
	}
	if out := screen(m); !strings.Contains(out, "proj55") {
		t.Fatalf("the cursor left the screen:\n%s", out)
	}
}

func TestTheNewSessionPopupStartsInTheProjectItWasOpenedFor(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true, Project: "/src/api"})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(withProjects(fixture(1, 0))))
	if out := screen(m); !strings.Contains(out, "‹ api ›") {
		t.Fatalf("the popup did not start in the project:\n%s", out)
	}
}
