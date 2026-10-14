package tui_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func switchModel(st *rpc.State, sw tui.Switcher) tui.Model {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Switch: sw})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(*st))
}

func escape() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

func TestModelSwitchMOpensThePickerWithTheSessionsHarnessModels(t *testing.T) {
	st := fixture(2, 0)
	m := switchModel(&st, &fakeSwitcher{})
	out := screen(press(m, "M"))
	for _, want := range []string{"opus", "sonnet", "haiku"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Claude picker misses %q:\n%s", want, out)
		}
	}
	out = screen(press(m, "j", "M"))
	if !strings.Contains(out, "gpt-6-luna") || strings.Contains(out, "opus") {
		t.Fatalf("Codex session:\n%s", out)
	}
}

func TestModelSwitchEOpensTheEffortLevels(t *testing.T) {
	st := fixture(1, 0)
	out := screen(press(switchModel(&st, &fakeSwitcher{}), "E"))
	if !strings.Contains(out, "xhigh") || strings.Contains(out, "sonnet") {
		t.Fatalf("effort picker:\n%s", out)
	}
}

func TestModelSwitchEnterAppliesTheHighlightedChoice(t *testing.T) {
	st := fixture(1, 0)
	sw := &fakeSwitcher{}
	m := press(switchModel(&st, sw), "M", "j")
	next, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 model sonnet" {
		t.Fatalf("calls %q", sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "haiku") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestModelSwitchNumberKeyPicksThatChoice(t *testing.T) {
	st := fixture(1, 0)
	sw := &fakeSwitcher{}
	m := press(switchModel(&st, sw), "E")
	_, cmd := m.Update(key("3"))
	if cmd == nil {
		t.Fatal("no command")
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 effort high" {
		t.Fatalf("calls %q", sw.calls)
	}
}

func TestModelSwitchEscapeClosesThePickerWithoutSwitching(t *testing.T) {
	st := fixture(1, 0)
	sw := &fakeSwitcher{}
	m := press(switchModel(&st, sw), "M")
	next, cmd := m.Update(escape())
	if cmd != nil || len(sw.calls) != 0 {
		t.Fatalf("cmd %v, calls %q", cmd, sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "haiku") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestModelSwitchPickerKeepsOtherKeysFromMovingTheSelection(t *testing.T) {
	st := fixture(3, 0)
	m := switchModel(&st, &fakeSwitcher{})
	first := m.Selected()
	if m = press(m, "M", "q", "tab", "space"); m.Selected() != first {
		t.Fatalf("selection moved to %q", m.Selected())
	}
}

func TestModelSwitchWithoutASessionDoesNothing(t *testing.T) {
	m := switchModel(&rpc.State{}, &fakeSwitcher{})
	if out := screen(press(m, "M")); strings.Contains(out, "opus") {
		t.Fatalf("picker opened:\n%s", out)
	}
}

func TestModelSwitchFailureShowsInTheFooter(t *testing.T) {
	st := fixture(1, 0)
	sw := &fakeSwitcher{err: errors.New("daemon says no")}
	m := press(switchModel(&st, sw), "M")
	_, cmd := m.Update(key("1"))
	msg := cmd()
	m = update(m, msg)
	if out := screen(m); !strings.Contains(out, "daemon says no") {
		t.Fatalf("no error in footer:\n%s", out)
	}
}

func TestModelSwitchSidebarShowsPendingAndWarning(t *testing.T) {
	st := fixture(2, 0)
	st.Sessions[0].Switches = []domain.Switch{{Kind: domain.SwitchModel, Value: "sonnet"}}
	st.Sessions[1].SwitchWarning = true
	m := switchModel(&st, &fakeSwitcher{})
	out := screen(m)
	var pending, warned bool
	for _, line := range strings.Split(out, "\n") {
		pending = pending || strings.Contains(line, "opus-5.5") && strings.Contains(line, "→ sonnet")
	}
	out = screen(press(m, "j"))
	for _, line := range strings.Split(out, "\n") {
		warned = warned || strings.Contains(line, "gpt-6") && strings.Contains(line, "!")
	}
	if !pending || !warned {
		t.Fatalf("pending %v, warned %v:\n%s", pending, warned, out)
	}
}

func TestModelSwitchDefaultsComeFromConfigPerHarness(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	if err := os.WriteFile(path, []byte("[defaults.claude]\nmodel = \"sonnet\"\neffort = \"high\"\n[defaults.codex]\nmodel = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := tui.LoadDefaults(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := d[domain.HarnessClaude]; got.Model != "sonnet" || got.Effort != "high" {
		t.Fatalf("claude %+v", got)
	}
	if got := d[domain.HarnessCodex]; got.Model != "gpt-5" || got.Effort != "" {
		t.Fatalf("codex %+v", got)
	}
	none, err := tui.LoadDefaults(t.TempDir() + "/missing.toml")
	if err != nil || len(none) != 0 {
		t.Fatalf("missing file gave %+v, %v", none, err)
	}
}
