package tui_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

var folders = map[string][]domain.Child{
	"/":              {{Name: "code", Path: "/code"}, {Name: "home", Path: "/home"}},
	"/code":          {{Name: "shop", Path: "/code/shop"}},
	"/code/shop":     {{Name: "api", Path: "/code/shop/api", Git: domain.GitDir}, {Name: "web", Path: "/code/shop/web", Git: domain.GitDir}, {Name: ".git", Path: "/code/shop/.git"}},
	"/code/shop/web": {{Name: "src", Path: "/code/shop/web/src"}},
	"/home/me":       {{Name: "notes", Path: "/home/me/notes"}},
	"/src":           {{Name: "api", Path: "/src/api", Git: domain.GitDir}, {Name: "shop", Path: "/src/shop"}},
}

func pathModel(t *testing.T, st rpc.State) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{dirs: folders}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true, LaunchDir: "/code/shop", Home: "/home/me"})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = update(m, tui.StateMsg(st))
	return pressCmd(typePath(m, "fix it"), keyTab), c
}

func typePath(m tui.Model, s string) tui.Model {
	next, cmd := m.Update(tea.PasteMsg{Content: s})
	return run(next.(tui.Model), cmd)
}

func submitted(t *testing.T, c *fakeCaller) rpc.NewSessionParams {
	t.Helper()
	for _, call := range c.calls {
		if call.method == rpc.MethodNewSession {
			return call.params.(rpc.NewSessionParams)
		}
	}
	t.Fatalf("no session.new in %v", c.methods())
	return rpc.NewSessionParams{}
}

func TestNewSessionPathListsTheFoldersWhereThePopupOpened(t *testing.T) {
	m, _ := pathModel(t, rpc.State{})
	out := screen(typePath(m, "./"))
	for _, want := range []string{"./", "api", "web", "/code/shop"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the dropdown:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".git") {
		t.Errorf("hidden folders listed without a dot prefix:\n%s", out)
	}
	if out := screen(typePath(m, "./w")); strings.Contains(out, "api") || !strings.Contains(out, "web") {
		t.Errorf("the prefix does not narrow the dropdown:\n%s", out)
	}
}

func TestNewSessionPathOpensTheHighlightedFolderAndStartsThere(t *testing.T) {
	m, c := pathModel(t, rpc.State{})
	m = pressCmd(pressCmd(typePath(m, "./"), keyDown), keyRight)
	out := screen(m)
	if !strings.Contains(out, "./web/") || !strings.Contains(out, "src") {
		t.Fatalf("right does not open the highlighted folder:\n%s", out)
	}
	pressCmd(m, keyEnter)
	if got := submitted(t, c).Workspace; got != "/code/shop/web" {
		t.Fatalf("workspace %q, want /code/shop/web", got)
	}
}

func TestNewSessionPathUpOneLevelWithLeft(t *testing.T) {
	m, _ := pathModel(t, rpc.State{})
	m = pressCmd(typePath(m, "./web/"), keyLeft)
	if out := screen(m); !strings.Contains(out, "│ ./▏") && !strings.Contains(out, "│ ./ ") {
		t.Fatalf("left does not go up a level:\n%s", out)
	}
	m = pressCmd(pressCmd(m, keyDown), keyUp)
	if out := screen(pressCmd(m, keyRight)); !strings.Contains(out, "./api/") {
		t.Fatalf("up does not move the highlight back to api:\n%s", out)
	}
}

func TestNewSessionPathResolvesParentsAndHome(t *testing.T) {
	m, c := pathModel(t, rpc.State{})
	if out := screen(typePath(m, "../../")); !strings.Contains(out, "code") || !strings.Contains(out, "home") {
		t.Fatalf("../../ does not list the root:\n%s", out)
	}
	m = typePath(m, "~/no")
	if out := screen(m); !strings.Contains(out, "notes") || !strings.Contains(out, "/home/me/no") {
		t.Fatalf("~ is not home:\n%s", out)
	}
	pressCmd(pressCmd(m, keyRight), keyEnter)
	if got := submitted(t, c).Workspace; got != "/home/me/notes" {
		t.Fatalf("workspace %q, want /home/me/notes", got)
	}
}

func TestNewSessionPathSaysWhatTheFolderIs(t *testing.T) {
	m, _ := pathModel(t, repoWorkspaces(rpc.State{}))
	if out := screen(typePath(m, "/src/api")); !strings.Contains(out, "single repo") {
		t.Errorf("a registered workspace is not named as one:\n%s", out)
	}
	if out := screen(typePath(m, "./web")); !strings.Contains(out, "new · added when you create") {
		t.Errorf("an unregistered folder is not called new:\n%s", out)
	}
	if out := screen(typePath(m, "./nope")); !strings.Contains(out, "no such folder") {
		t.Errorf("a missing folder is not flagged:\n%s", out)
	}
}

func TestNewSessionPathEmptiedGoesBackToTheWorkspacePicker(t *testing.T) {
	m, c := pathModel(t, repoWorkspaces(rpc.State{}))
	m = pressCmd(pressCmd(typePath(m, "."), keyBack), keyRight)
	if out := screen(m); !strings.Contains(out, "‹ shop ›") && !strings.Contains(out, "‹ api ›") {
		t.Fatalf("an emptied path does not bring the picker back:\n%s", out)
	}
	pressCmd(m, keyEnter)
	if got := submitted(t, c).Workspace; !strings.HasPrefix(got, "/src/") && got != "/code/shop" {
		t.Fatalf("workspace %q", got)
	}
}

func TestNewSessionPathRetriesAFailedListingOnTheNextEdit(t *testing.T) {
	m, c := pathModel(t, rpc.State{})
	c.err, c.failOn = errors.New("daemon busy"), rpc.MethodWorkspaceDirs
	m = typePath(m, "./")
	c.err = nil
	if out := screen(typePath(m, "w")); !strings.Contains(out, "web/") {
		t.Fatalf("a failed listing is never retried:\n%s", out)
	}
}

func TestNewSessionPathKeepsTheNewestListingOfAFolder(t *testing.T) {
	m, c := pathModel(t, rpc.State{})
	next, older := m.Update(tea.PasteMsg{Content: "./"})
	m = pressCmd(next.(tui.Model), keyLeft)
	next, newer := m.Update(tea.PasteMsg{Content: "shop/"})
	m = next.(tui.Model)
	stale := older()
	c.dirs = map[string][]domain.Child{"/code/shop": {{Name: "fresh", Path: "/code/shop/fresh"}}}
	m = update(update(m, newer()), stale)
	if out := screen(m); !strings.Contains(out, "fresh/") {
		t.Fatalf("an older reply for the same folder replaced the newer one:\n%s", out)
	}
}
