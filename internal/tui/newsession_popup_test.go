package tui_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

var popupCommand = []string{"/bin/crew", "tui", "--new-session"}

func repoWorkspaces(st rpc.State) rpc.State {
	st.Workspaces = []domain.Workspace{
		{Root: "/src/api", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "api", Path: "/src/api", DefaultBranch: "main"}}},
		{Root: "/src/shop", Kind: domain.WorkspaceOrchestration, LastUsed: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
			Repos: []domain.Repo{{Name: "web", Path: "/src/shop/web"}, {Name: "infra", Path: "/src/shop/infra"}}},
	}
	return st
}

func popupModel(t *testing.T, st rpc.State, width int) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true})
	m = update(m, tea.WindowSizeMsg{Width: width, Height: 30})
	return update(m, tui.StateMsg(st)), c
}

func quits(cmd tea.Cmd) bool {
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return false
		}
		cmd = nil
		for _, c := range batch {
			if quits(c) {
				return true
			}
		}
	}
	return false
}

func TestNewSessionPopupLooksLikeTheMockup(t *testing.T) {
	m, _ := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	out := screen(m)
	for _, want := range []string{"New session", "esc", "Task (optional)", "Workspace", "Harness", "Model", "Effort", "● claude", "○ codex", "cancel", "⏎ create"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the popup:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SESSIONS") {
		t.Fatalf("the popup draws the sidebar:\n%s", out)
	}
}

func TestNewSessionPopupPreviewsTheWorkItem(t *testing.T) {
	cases := []struct{ input, want string }{
		{"https://linear.app/acme/issue/ENG-9", "text · session named “https://linear.app/acme/issue/ENG-9”"},
		{"https://github.com/acme/api/pull/12", "api#12 · session will be named after the PR"},
		{"fix the flaky upload test", "text · session named “fix the flaky upload test”"},
	}
	for _, c := range cases {
		m, _ := popupModel(t, repoWorkspaces(rpc.State{}), 100)
		if out := screen(typeText(m, c.input)); !strings.Contains(out, c.want) {
			t.Errorf("%s: no %q:\n%s", c.input, c.want, out)
		}
	}
}

func TestNewSessionPopupPlaceholderNamesOnlyAPROrText(t *testing.T) {
	m, _ := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	m = pressCmd(m, keyTab)
	if out := screen(m); !strings.Contains(out, "a GitHub PR or a few words") {
		t.Fatalf("placeholder:\n%s", out)
	}
}

func TestNewSessionPopupDescribesTheWorkspaceAndWhereTheSessionStarts(t *testing.T) {
	m, _ := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	m = typeText(m, "add search")
	out := screen(m)
	for _, want := range []string{"‹ shop ›", "orchestration root · 2 repos", "last used", "Starts at the shop root", "The agent creates worktrees as it needs them"} {
		if !strings.Contains(out, want) {
			t.Errorf("orchestration root: no %q:\n%s", want, out)
		}
	}
	m = pressCmd(pressCmd(m, keyTab), keyLeft)
	out = screen(m)
	for _, want := range []string{"‹ api ›", "single repo", "Starts in a fresh worktree", "api:add-search from origin/main"} {
		if !strings.Contains(out, want) {
			t.Errorf("single repo: no %q:\n%s", want, out)
		}
	}
}

func TestNewSessionPopupPicksTheModelFromTheHarnessChoices(t *testing.T) {
	m, c := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	m = typeText(m, "x")
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyTab)
	if out := screen(m); !strings.Contains(out, "‹ default (opus) ›") {
		t.Fatalf("model does not start at the default:\n%s", out)
	}
	m = pressCmd(m, keyRight)
	if out := screen(m); !strings.Contains(out, "‹ opus ›") {
		t.Fatalf("right does not pick the first Claude model:\n%s", out)
	}
	pressCmd(m, keyEnter)
	if p, ok := c.calls[0].params.(rpc.NewSessionParams); !ok || p.Model != "opus" {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestNewSessionPopupWarnsInABoxAndOffersCodex(t *testing.T) {
	m, _ := popupModel(t, withWorkspaces(lowClaudeState()), 100)
	out := screen(m)
	for _, want := range []string{"Claude 5h window 90% used", "ctrl+s start in codex", "Codex 5h is 36% used"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if out := screen(pressCmd(m, keyCtrlS)); !strings.Contains(out, "● codex") {
		t.Fatalf("ctrl+s did not switch to codex:\n%s", out)
	}
}

func TestNewSessionPopupQuitsOnEsc(t *testing.T) {
	m, c := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	if _, cmd := m.Update(keyEsc); !quits(cmd) {
		t.Fatal("esc does not close the popup")
	}
	if len(c.methods()) != 0 {
		t.Fatalf("calls %v", c.methods())
	}
}

func TestNewSessionPopupShowsTheNewSessionAndQuits(t *testing.T) {
	m, c := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	m = typeText(m, "add search")
	next, cmd := m.Update(keyEnter)
	msg := cmd()
	_, cmd = next.Update(msg)
	if !quits(cmd) {
		t.Fatal("the popup stays open after the session started")
	}
	if !slices.Equal(c.methods(), []string{rpc.MethodNewSession, rpc.MethodSessionFocus}) {
		t.Fatalf("calls %v; want the session started then focused", c.methods())
	}
}

func TestNOpensTheDialogAsAPopupWhenOneIsConfigured(t *testing.T) {
	st := repoWorkspaces(fixture(1, 0))
	c := &fakeCaller{}
	env := map[string]string{"CREW_HOME": "/h"}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, DialogPopup: rpc.ClientPopupParams{Command: popupCommand, Env: env}})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	m = pressCmd(m, key("n"))
	want := rpc.ClientPopupParams{Command: popupCommand, Env: env}
	if !slices.Equal(c.methods(), []string{rpc.MethodClientPopup}) || !reflect.DeepEqual(c.calls[0].params, want) {
		t.Fatalf("calls %+v", c.calls)
	}
	if strings.Contains(screen(m), "New session") {
		t.Fatalf("the dialog also opened inline:\n%s", screen(m))
	}

	c.err = errors.New("no terminal is attached")
	if out := screen(pressCmd(m, key("n"))); !strings.Contains(out, "New session") {
		t.Fatalf("no inline dialog when the popup fails:\n%s", out)
	}
}

func TestNewSessionPopupKeepsTheActionsInViewInAShortWindow(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true})
	m = update(m, tea.WindowSizeMsg{Width: 60, Height: 16})
	m = update(m, tui.StateMsg(withWorkspaces(lowClaudeState())))
	m = pressCmd(pressCmd(pressCmd(typeText(m, "x"), keyTab), keyTab), keyTab)
	out := screen(m)
	if lines := strings.Split(out, "\n"); len(lines) > 16 {
		t.Fatalf("%d rows in a 16-row popup:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "Model") || !strings.Contains(out, "⏎ create") {
		t.Fatalf("the active field or the actions are cut off:\n%s", out)
	}
}

func TestNewSessionPopupSaysSoWhenTheNewSessionCannotBeShown(t *testing.T) {
	m, c := popupModel(t, repoWorkspaces(rpc.State{}), 100)
	c.err, c.failOn = errors.New("no client layout is open"), rpc.MethodSessionFocus
	m = typeText(m, "add search")
	next, cmd := m.Update(keyEnter)
	next, cmd = next.Update(cmd())
	if quits(cmd) {
		t.Fatal("the popup closed without saying the session could not be shown")
	}
	out := screen(run(next.(tui.Model), cmd))
	if !strings.Contains(out, "started, but showing it failed") || !strings.Contains(out, "no client layout is open") {
		t.Fatalf("no error in the popup:\n%s", out)
	}
}

func launchedIn(t *testing.T, dir string, st rpc.State) tui.Model {
	t.Helper()
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: &fakeCaller{}, NewSessionOnly: true, LaunchDir: dir})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return update(m, tui.StateMsg(st))
}

func TestNewSessionPopupDefaultsToTheFolderCrewWasLaunchedIn(t *testing.T) {
	out := screen(launchedIn(t, "/code/web", rpc.State{}))
	if !strings.Contains(out, "‹ web ›") || !strings.Contains(out, "new · added when you create") || strings.Contains(out, "workspace add") {
		t.Fatalf("want the launch folder as a new workspace:\n%s", out)
	}
	if out := screen(launchedIn(t, "/src/api/internal", repoWorkspaces(rpc.State{}))); !strings.Contains(out, "‹ api ›") || strings.Contains(out, "added when you create") {
		t.Fatalf("want the registered workspace that holds the launch folder:\n%s", out)
	}
}

func TestNewSessionPopupStartsInTheNewFolder(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true, LaunchDir: "/code/web"})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, tui.StateMsg(rpc.State{}))
	pressCmd(typeText(m, "fix it"), keyEnter)
	if len(c.calls) == 0 || c.calls[0].params.(rpc.NewSessionParams).Workspace != "/code/web" {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestNewSessionPopupPrefersTheMostSpecificWorkspace(t *testing.T) {
	st := rpc.State{Workspaces: []domain.Workspace{
		{Root: "/src", Kind: domain.WorkspaceOrchestration},
		{Root: "/src/api", Kind: domain.WorkspaceSingle},
	}}
	if out := screen(launchedIn(t, "/src/api/internal", st)); !strings.Contains(out, "‹ api ›") {
		t.Fatalf("want /src/api, not its parent /src:\n%s", out)
	}
}

func TestNewSessionPopupNamesWhatDefaultMeans(t *testing.T) {
	st := repoWorkspaces(rpc.State{Sessions: []domain.Session{{ID: "s", Harness: domain.HarnessClaude, Model: "opus-5.5"}}})
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, NewSessionOnly: true,
		HarnessDefaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {Effort: "low"}}})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, tui.StateMsg(st))
	if out := screen(m); !strings.Contains(out, "‹ default (opus) ›") || strings.Contains(out, "opus-5.5") || !strings.Contains(out, "‹ default (low) ›") {
		t.Fatalf("defaults not named:\n%s", out)
	}
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyRight)
	if out := screen(m); !strings.Contains(out, "‹ default (gpt-6.1-sol) ›") || !strings.Contains(out, "‹ default (medium) ›") {
		t.Fatalf("codex, with nothing configured, should name its built-in default:\n%s", out)
	}
}

func TestNewSessionPopupNamesTheSameDefaultEveryTime(t *testing.T) {
	st := repoWorkspaces(rpc.State{Sessions: []domain.Session{
		{ID: "a", Harness: domain.HarnessClaude, Model: "sonnet-5.5"},
		{ID: "b", Harness: domain.HarnessClaude, Model: "opus-5.5"},
	}})
	for range 20 {
		m, _ := popupModel(t, st, 100)
		if out := screen(m); !strings.Contains(out, "‹ default (opus) ›") || strings.Contains(out, "opus-5.5") || strings.Contains(out, "sonnet-5.5") {
			t.Fatalf("the blank model should name the built-in default and no session model:\n%s", out)
		}
	}
}

func TestNewSessionPopupNamesThePerModelEffortOfTheDefaultModel(t *testing.T) {
	st := repoWorkspaces(rpc.State{Sessions: []domain.Session{{ID: "s", Harness: domain.HarnessClaude, Model: "opus-5.5"}}})
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: &fakeCaller{}, NewSessionOnly: true,
		HarnessDefaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {EffortByModel: map[string]string{"opus-5.5": "low", "sonnet-5.5": "high"}}}})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, tui.StateMsg(st))
	if out := screen(m); !strings.Contains(out, "‹ default (opus) ›") || strings.Contains(out, "opus-5.5") || !strings.Contains(out, "‹ default (low) ›") {
		t.Fatalf("want the effort set for opus-5.5:\n%s", out)
	}
}

func TestNewSessionPopupNamesTheConfiguredDefaultModel(t *testing.T) {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: &fakeCaller{}, NewSessionOnly: true,
		HarnessDefaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {Model: "sonnet", Effort: "xhigh"}}})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, tui.StateMsg(repoWorkspaces(rpc.State{})))
	if out := screen(m); !strings.Contains(out, "‹ default (sonnet) ›") || !strings.Contains(out, "‹ default (xhigh) ›") {
		t.Fatalf("configured defaults not named:\n%s", out)
	}
}
