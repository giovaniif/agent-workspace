package tui_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
)

var setupCommand = []string{"/bin/agentws", "tui", "--setup"}

func freshMachine() domain.Onboarding {
	return domain.Onboarding{
		Harnesses: map[domain.Harness]domain.HarnessSetup{
			domain.HarnessClaude: {File: "/Users/me/.claude/settings.json", Backup: "/Users/me/.claude/settings.json.agentws-backup"},
			domain.HarnessCodex:  {File: "/Users/me/.codex/hooks.json", Backup: "/Users/me/.codex/hooks.json.agentws-<time>.bak"},
			domain.HarnessOmp:    {File: "/Users/me/.omp/agent/hooks/post/agentws.ts"},
		},
		Nvim: domain.NvimSetup{
			OnPath: true, ConfigFile: "/Users/me/.config/nvim/plugin/agentws.lua",
			PluginDir: "/Users/me/.local/share/agentws/nvim", PluginFound: true,
		},
	}
}

func setupModel(t *testing.T, o domain.Onboarding) (tui.Model, *fakeOnboarder) {
	t.Helper()
	f := &fakeOnboarder{state: o}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Onboard: f, SetupOnly: true})
	m = update(m, tea.WindowSizeMsg{Width: 90, Height: 32})
	next, cmd := m.Update(tui.StateMsg(rpc.State{}))
	return run(next.(tui.Model), cmd), f
}

func mustShow(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("no %q:\n%s", w, out)
		}
	}
}

func TestGoldenSetup(t *testing.T) {
	m, _ := setupModel(t, freshMachine())
	steps := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"pick", nil},
		{"claude", []tea.KeyPressMsg{keyDown, keySpace, keyEnter}},
		{"codex", []tea.KeyPressMsg{keyEnter, keyEnter}},
		{"nvim", []tea.KeyPressMsg{key("s")}},
		{"finish", []tea.KeyPressMsg{key("s")}},
	}
	for _, s := range steps {
		for _, k := range s.keys {
			m = pressCmd(m, k)
		}
		t.Run(s.name, func(t *testing.T) { golden.RequireEqual(t, screen(m)) })
	}
}

func TestSetupPickStepShowsEachHarnessState(t *testing.T) {
	o := freshMachine()
	o.Harnesses[domain.HarnessCodex] = domain.HarnessSetup{Installed: true, File: "/Users/me/.codex/hooks.json"}
	m, _ := setupModel(t, o)
	out := screen(m)
	mustShow(t, out, "Set up agentws", "esc skip", "Which agents do you use?", "Claude Code", "not set up", "Codex", "set up", "⏎ next")
	if strings.Contains(out, "SESSIONS") {
		t.Fatalf("the setup screen draws the sidebar:\n%s", out)
	}
	if !strings.Contains(out, "[x] Codex") || !strings.Contains(out, "[ ] Claude Code") {
		t.Errorf("the installed harness is not the one picked:\n%s", out)
	}
}

func TestSetupPickStepHasARowPerCatalogHarness(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	mustShow(t, screen(m), "[x] Claude Code", "[ ] Codex", "[ ] Oh My Pi")
	m = pressCmd(pressCmd(pressCmd(m, keyDown), keyDown), keySpace)
	m = pressCmd(pressCmd(pressCmd(m, keyUp), keyUp), keySpace)
	mustShow(t, screen(m), "[ ] Claude Code", "[x] Oh My Pi")
	m = pressCmd(m, keyEnter)
	mustShow(t, screen(m), "Oh My Pi", "/Users/me/.omp/agent/hooks/post/agentws.ts", "agentws setup omp --remove", "⏎ install")
	m = pressCmd(m, keyEnter)
	if !slices.Equal(f.installed, []domain.Harness{domain.HarnessOmp}) {
		t.Fatalf("installed = %v", f.installed)
	}
	mustShow(t, screen(pressCmd(m, keyEnter)), "Neovim")
}

func TestSetupInstallsOnlyThePickedHarnessesOnConfirmation(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	m = pressCmd(m, keyEnter)
	mustShow(t, screen(m), "Claude Code", "/Users/me/.claude/settings.json", "settings.json.agentws-backup", "agentws setup claude --remove", "⏎ install")
	if len(f.installed) != 0 {
		t.Fatalf("installed before confirmation: %v", f.installed)
	}
	m = pressCmd(m, keyEnter)
	if !slices.Equal(f.installed, []domain.Harness{domain.HarnessClaude}) {
		t.Fatalf("installed = %v", f.installed)
	}
	out := screen(m)
	mustShow(t, out, "✓ set up")
	m = pressCmd(m, keyEnter)
	if out := screen(m); !strings.Contains(out, "Neovim") {
		t.Fatalf("codex was not picked, yet the walkthrough did not go on to nvim:\n%s", out)
	}
}

func TestSetupCodexStepShowsTheTrustStep(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	m = pressCmd(pressCmd(pressCmd(pressCmd(m, keySpace), keyDown), keySpace), keyEnter)
	out := screen(m)
	mustShow(t, out, "Codex", "/Users/me/.codex/hooks.json", "Codex runs a hook only after you trust it")
	m = pressCmd(m, key("s"))
	if len(f.installed) != 0 {
		t.Fatalf("skip installed %v", f.installed)
	}
	mustShow(t, screen(m), "Neovim")
}

func TestSetupShowsAnInstallFailureAndStays(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	f.err = errors.New("settings.json is not valid JSON, left untouched")
	m = pressCmd(pressCmd(m, keyEnter), keyEnter)
	mustShow(t, screen(m), "✗ settings.json is not valid JSON", "Claude Code")
}

func TestSetupNvimStep(t *testing.T) {
	cases := []struct {
		name string
		nvim domain.NvimSetup
		want []string
		not  []string
	}{
		{"snippet", freshMachine().Nvim, []string{
			"require('agentws').setup({})",
			"/Users/me/.config/nvim/plugin/agentws.lua",
			"local dir = '/Users/me/.local/share/agentws/nvim'",
			"agentws setup nvim --remove",
			"⏎ write it",
		}, nil},
		{"configured", domain.NvimSetup{OnPath: true, Configured: true}, []string{"✓ the agentws plugin is configured"}, []string{"prepend"}},
		{"no nvim", domain.NvimSetup{}, []string{"optional", "nvim is not on PATH", "brew install neovim", "⏎ next"}, []string{"prepend", "s skip"}},
		{"no plugin dir", domain.NvimSetup{OnPath: true, PluginDir: "/Users/me/.local/share/agentws/nvim", ConfigFile: "/c/init.lua"}, []string{"plugin files are not at /Users/me/.local/share/agentws/nvim", "install script"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := freshMachine()
			o.Nvim = c.nvim
			m, _ := setupModel(t, o)
			m = pressCmd(pressCmd(m, keySpace), keyEnter)
			out := screen(m)
			mustShow(t, out, c.want...)
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("shows %q:\n%s", n, out)
				}
			}
		})
	}
}

func TestSetupFinishOrEscRecordsCompletionAndQuits(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	m = pressCmd(pressCmd(m, keySpace), keyEnter)
	m = pressCmd(m, key("s"))
	mustShow(t, screen(m), "You're set", "agentws setup")
	next, cmd := m.Update(keyEnter)
	if !quits(runUntilQuit(next.(tui.Model), cmd)) || f.finished != 1 {
		t.Fatalf("finish: finished %d", f.finished)
	}

	m, f = setupModel(t, freshMachine())
	next, cmd = m.Update(keyEsc)
	if !quits(runUntilQuit(next.(tui.Model), cmd)) || f.finished != 1 || len(f.installed) != 0 {
		t.Fatalf("esc: finished %d, installed %v", f.finished, f.installed)
	}
}

// why: the last command is returned so a test can see whether the program ends.
func runUntilQuit(m tui.Model, cmd tea.Cmd) tea.Cmd {
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok || msg == nil {
			return cmd
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(tui.Model)
	}
	return nil
}

func sidebarWithSetup(t *testing.T, o domain.Onboarding, c *fakeCaller) (tui.Model, *fakeOnboarder) {
	t.Helper()
	f := &fakeOnboarder{state: o}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c, Onboard: f, SetupPopup: rpc.ClientPopupParams{Command: setupCommand}})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	next, cmd := m.Update(tui.StateMsg(fixture(1, 0)))
	return run(next.(tui.Model), cmd), f
}

func TestFirstRunOpensTheSetupPopupOnce(t *testing.T) {
	c := &fakeCaller{}
	m, _ := sidebarWithSetup(t, freshMachine(), c)
	want := rpc.ClientPopupParams{Command: setupCommand}
	if !slices.Equal(c.methods(), []string{rpc.MethodClientPopup}) || !reflect.DeepEqual(c.calls[0].params, want) {
		t.Fatalf("calls %+v", c.calls)
	}
	next, cmd := m.Update(tui.StateMsg(fixture(1, 0)))
	run(next.(tui.Model), cmd)
	if len(c.methods()) != 1 {
		t.Fatalf("a later snapshot opened it again: %v", c.methods())
	}
}

func TestSetupDoneOpensNothingUntilS(t *testing.T) {
	c := &fakeCaller{}
	o := freshMachine()
	o.Done = true
	m, _ := sidebarWithSetup(t, o, c)
	if len(c.methods()) != 0 {
		t.Fatalf("calls %v after onboarding was done", c.methods())
	}
	if out := screen(press(m, "?")); !strings.Contains(out, "set up Claude, Codex and nvim") {
		t.Errorf("help does not list S:\n%s", out)
	}
	pressCmd(m, key("S"))
	if !slices.Equal(c.methods(), []string{rpc.MethodClientPopup}) {
		t.Fatalf("S calls %v", c.methods())
	}
}

func TestSetupOpensInlineWhenThePopupCannot(t *testing.T) {
	c := &fakeCaller{err: errors.New("no client is attached"), failOn: rpc.MethodClientPopup}
	m, _ := sidebarWithSetup(t, freshMachine(), c)
	mustShow(t, screen(m), "Set up agentws", "Which agents do you use?")
	m = pressCmd(m, keyEsc)
	if out := screen(m); strings.Contains(out, "Set up agentws") || !strings.Contains(out, "SESSIONS") {
		t.Fatalf("esc did not close the inline setup:\n%s", out)
	}
}

func TestSetupNvimStepAddsTheBlockOnlyOnConfirmation(t *testing.T) {
	m, f := setupModel(t, freshMachine())
	m = pressCmd(pressCmd(m, keySpace), keyEnter)
	if f.nvimInstalls != 0 {
		t.Fatal("nvim config written before confirmation")
	}
	m = pressCmd(m, keyEnter)
	if f.nvimInstalls != 1 {
		t.Fatalf("installs = %d", f.nvimInstalls)
	}
	mustShow(t, screen(m), "✓ wrote /Users/me/.config/nvim/plugin/agentws.lua", "agentws setup nvim --remove", "⏎ next")
	mustShow(t, screen(pressCmd(m, keyEnter)), "You're set", "plugin configured")

	m, f = setupModel(t, freshMachine())
	m = pressCmd(pressCmd(pressCmd(m, keySpace), keyEnter), key("s"))
	if f.nvimInstalls != 0 {
		t.Fatal("s wrote the nvim config")
	}
	mustShow(t, screen(m), "You're set")
}

func TestAlreadySetUpMachinesSkipTheWalkthroughAndRecordIt(t *testing.T) {
	c := &fakeCaller{}
	o := freshMachine()
	o.Harnesses[domain.HarnessCodex] = domain.HarnessSetup{Installed: true, File: "/Users/me/.codex/hooks.json"}
	o.Nvim.Configured = true
	m, f := sidebarWithSetup(t, o, c)
	if len(c.methods()) != 0 || f.finished != 1 {
		t.Fatalf("calls %v, finished %d", c.methods(), f.finished)
	}
	if strings.Contains(screen(m), "Set up agentws") {
		t.Fatalf("walkthrough shown:\n%s", screen(m))
	}
}
