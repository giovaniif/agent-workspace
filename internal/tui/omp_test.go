package tui_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

func ompState() rpc.State {
	st := fixture(1, 0)
	st.Sessions[0].Harness, st.Sessions[0].Model, st.Sessions[0].Effort = domain.HarnessOmp, "anthropic/opus", "high"
	return st
}

func TestNewSessionDialogCyclesEveryCatalogHarness(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(fixture(1, 0)))
	m = typeText(m, "add search")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	m = pressCmd(pressCmd(m, keyRight), keyRight)
	if out := screen(m); !strings.Contains(out, "omp") {
		t.Fatalf("two steps right is not omp:\n%s", out)
	}
	if got := startedParams(t, m, c).Harness; got != "omp" {
		t.Fatalf("harness %q", got)
	}
	c.calls = nil
	if got := startedParams(t, pressCmd(m, keyRight), c).Harness; got != "claude" {
		t.Fatalf("after omp came %q, want claude", got)
	}
}

func ompModelDialog(t *testing.T, choices []string) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Calls: c,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: choices},
	})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	m = press(m, "n")
	m = typeText(m, "add search")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	return pressCmd(pressCmd(m, keyLeft), keyTab), c
}

func TestNewSessionDialogCyclesOmpModelsWithArrows(t *testing.T) {
	m, c := ompModelDialog(t, []string{"alpha/one", "beta/two"})
	m = pressCmd(m, keyRight)
	if out := screen(m); !strings.Contains(out, "‹ alpha/one ›") || strings.Contains(out, "▸") {
		t.Fatalf("right should cycle, not open a menu:\n%s", out)
	}
	m = pressCmd(m, keyRight)
	if out := screen(m); !strings.Contains(out, "‹ beta/two ›") {
		t.Fatalf("next model:\n%s", out)
	}
	m = typeText(m, "nope")
	m = pressCmd(m, keyRight)
	if out := screen(m); strings.Contains(out, "nope") || strings.Contains(out, "▸") {
		t.Fatalf("right leaves typing and cycles:\n%s", out)
	}
	if got := startedParams(t, m, c).Model; got != "" {
		t.Fatalf("model %q", got)
	}
}

func TestNewSessionDialogCompletesAnOmpModelAsYouType(t *testing.T) {
	m, c := ompModelDialog(t, []string{"alpha/one", "beta/two", "beta/three"})
	m = typeText(m, "beta")
	out := screen(m)
	if !strings.Contains(out, "beta/two") || !strings.Contains(out, "beta/three") || strings.Contains(out, "alpha/one") {
		t.Fatalf("completions:\n%s", out)
	}
	if !menuFloatsOver(out, "beta/two") {
		t.Fatalf("menu is not floating over the form:\n%s", out)
	}
	m = pressCmd(m, keyCtrlN)
	if out := screen(m); !strings.Contains(out, "beta/two") || !strings.Contains(out, "beta/three") {
		t.Fatalf("menu closed on the first match:\n%s", out)
	}
	m = pressCmd(m, keyCtrlN)
	if out := screen(m); !strings.Contains(out, "beta/three") {
		t.Fatalf("ctrl-n:\n%s", out)
	}
	m = pressCmd(m, keyCtrlE)
	if out := screen(m); strings.Contains(out, "beta") || !strings.Contains(out, "‹ default ›") {
		t.Fatalf("ctrl-e should restore the arrows:\n%s", out)
	}
	m = typeText(m, "beta")
	m = pressCmd(pressCmd(pressCmd(m, keyCtrlN), keyCtrlN), keyCtrlY)
	if out := screen(m); !strings.Contains(out, "‹ beta/three ›") || strings.Contains(out, "beta/two") {
		t.Fatalf("ctrl-y should fill the cycled field:\n%s", out)
	}
	got := startedParams(t, m, c)
	if got.Harness != "omp" || got.Model != "beta/three" {
		t.Fatalf("params %+v", got)
	}
}

func menuFloatsOver(out, item string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "╰") && strings.Contains(line, "shop") {
			return true
		}
		if strings.Contains(line, "│") && strings.Contains(line, item) && strings.Contains(line, "default") {
			return true
		}
	}
	return false
}

func narrowRule(out string) int {
	best := 0
	for _, line := range strings.Split(out, "\n") {
		start := strings.LastIndex(line, "╭")
		end := strings.LastIndex(line, "╮")
		if start < 0 || end <= start {
			start = strings.LastIndex(line, "╰")
			end = strings.LastIndex(line, "╯")
		}
		if start < 0 || end <= start {
			continue
		}
		w := ansi.StringWidth(line[start : end+len("╮")])
		if best == 0 || w < best {
			best = w
		}
	}
	return best
}

func TestNewSessionMenuKeepsItsWidthAsTheQueryChanges(t *testing.T) {
	long := "provider/a-very-long-model-id"
	m := ompPopup(t, []string{long, "beta/two"}, 100, 40)
	wide := narrowRule(screen(typeText(m, "provider")))
	narrow := narrowRule(screen(typeText(m, "beta")))
	want := ansi.StringWidth(long) + 4
	if wide != want || narrow != want {
		t.Fatalf("menu width %d for the long match and %d for the short one, want %d\nlong:\n%s\nshort:\n%s",
			wide, narrow, want, screen(typeText(m, "provider")), screen(typeText(m, "beta")))
	}
}

func TestNewSessionMenuFloatsPastTheModal(t *testing.T) {
	m := ompPopup(t, []string{"alpha/one", "beta/two", "beta/three", "beta/four", "beta/five", "beta/six"}, 100, 16)
	out := screen(typeText(m, "beta"))
	top, bottom := -1, -1
	for i, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "╭") && strings.Contains(line, "╮") && ansi.StringWidth(ruleSpan(line, "╭", "╮")) < 40 {
			top = i
		}
		if strings.Contains(line, "╰") && strings.Contains(line, "╯") && ansi.StringWidth(ruleSpan(line, "╰", "╯")) < 40 {
			bottom = i
		}
	}
	if top < 0 || bottom <= top {
		t.Fatalf("menu border is cut (top %d bottom %d):\n%s", top, bottom, out)
	}
	for _, name := range []string{"beta/two", "beta/three", "beta/four", "beta/five", "beta/six"} {
		if !strings.Contains(out, name) {
			t.Fatalf("missing %s\n%s", name, out)
		}
	}
}

func TestNewSessionFrameMatchesTheForm(t *testing.T) {
	m := ompPopup(t, nil, 100, 30)
	out := screen(m)
	lines := strings.Split(out, "\n")
	width := ansi.StringWidth(lines[0])
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("line %d is %d columns, the title is %d:\n%s", i, got, width, out)
		}
	}
	box := 0
	for _, line := range lines {
		if strings.Contains(line, "╭") {
			box = ansi.StringWidth(line)
			break
		}
	}
	if box != width {
		t.Fatalf("work item border is %d columns, the title is %d:\n%s", box, width, out)
	}
}

func ompPopup(t *testing.T, choices []string, width, height int) tui.Model {
	t.Helper()
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Calls: &fakeCaller{}, NewSessionOnly: true,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: choices},
	})
	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	m = typeText(m, "add search")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	return pressCmd(pressCmd(m, keyLeft), keyTab)
}

func ruleSpan(line, left, right string) string {
	a := strings.LastIndex(line, left)
	b := strings.LastIndex(line, right)
	if a < 0 || b <= a {
		return ""
	}
	return line[a : b+len(right)]
}

func TestNewSessionMenuStartsAtTheModelField(t *testing.T) {
	m, _ := ompModelDialog(t, []string{"alpha/one", "beta/two"})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = typeText(m, "beta")
	field, box := -1, -1
	for _, line := range strings.Split(screen(m), "\n") {
		if i := strings.Index(line, "beta▏"); i >= 0 {
			field = ansi.StringWidth(line[:i])
		}
		if i := strings.Index(line, "╭"); i >= 0 && strings.Contains(line, "╮") {
			box = ansi.StringWidth(line[:i])
		}
	}
	if field < 0 || box < 0 || field != box {
		t.Fatalf("field column %d, menu column %d\n%s", field, box, screen(m))
	}
}

func TestNewSessionDialogDoesNotCompleteClaudeModels(t *testing.T) {
	m, _ := dialogModel(t, withWorkspaces(fixture(1, 0)))
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyTab)
	m = typeText(m, "nope")
	out := screen(m)
	if strings.Contains(out, "nope") || strings.Contains(out, "▸") {
		t.Fatalf("claude model took typed text:\n%s", out)
	}
	m = pressCmd(m, keyRight)
	if out := screen(m); !strings.Contains(out, "‹ opus ›") {
		t.Fatalf("right:\n%s", out)
	}
}

func TestNewSessionDialogKeepsALongModelInsideItsColumn(t *testing.T) {
	c := &fakeCaller{}
	long := strings.Repeat("m", 120) + "TAIL"
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Calls: c,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: {"prov/a"}},
	})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	m = press(m, "n")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	m = pressCmd(pressCmd(m, keyLeft), keyTab)
	m = typeText(m, long)
	out := screen(m)
	if strings.Contains(out, "TAIL") || !strings.Contains(out, "…") {
		t.Fatalf("model spills out of its column:\n%s", out)
	}
	if !strings.Contains(out, "Effort") || !strings.Contains(out, "default") {
		t.Fatalf("effort column:\n%s", out)
	}
}

func TestNewSessionDialogTakesATypedModelForOmp(t *testing.T) {
	m, c := dialogModel(t, withWorkspaces(fixture(1, 0)))
	m = typeText(m, "add search")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	m = pressCmd(pressCmd(m, keyLeft), keyTab)
	m = typeText(m, "anthropic/opus-x")
	m = pressCmd(m, keyBack)
	got := startedParams(t, m, c)
	want := rpc.NewSessionParams{Workspace: "/src/shop", WorkItem: "add search", Harness: "omp", Model: "anthropic/opus-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params %+v, want %+v", got, want)
	}
}

func TestSidebarTagsAnOmpSessionOM(t *testing.T) {
	st := ompState()
	out := screen(newModel(&st, nil))
	if !strings.Contains(out, " OM") || strings.Contains(out, " CC") {
		t.Fatalf("omp row:\n%s", ansi.Strip(out))
	}
}

func TestModelSwitchMOnOmpTakesATypedModel(t *testing.T) {
	st := ompState()
	sw := &fakeSwitcher{}
	m := press(switchModel(&st, sw), "M", "o", "p", "u", "s", "x")
	m = pressCmd(m, keyBack)
	next, cmd := m.Update(keyEnter)
	if cmd == nil {
		t.Fatalf("enter returned no command:\n%s", screen(m))
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 model opus" {
		t.Fatalf("calls %q", sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "MODEL") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestModelSwitchMOnOmpListsModels(t *testing.T) {
	st := ompState()
	sw := &fakeSwitcher{}
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Switch: sw,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: {"prov/a", "prov/b"}},
	})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	out := screen(press(m, "M"))
	if !strings.Contains(out, "prov/a") || !strings.Contains(out, "prov/b") || strings.Contains(out, "type a model id") {
		t.Fatalf("picker:\n%s", out)
	}
	next, cmd := press(m, "M").Update(keyEnter)
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 model prov/a" {
		t.Fatalf("calls %q", sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "MODEL") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestNewSessionDialogEnterAcceptsTheHighlightedModel(t *testing.T) {
	m, c := ompModelDialog(t, []string{"alpha/one", "beta/two", "beta/three"})
	m = typeText(m, "beta")
	m = pressCmd(pressCmd(m, keyCtrlN), keyCtrlN)
	if got := startedParams(t, m, c).Model; got != "beta/three" {
		t.Fatalf("model %q", got)
	}
}

func TestNewSessionDialogCtrlPMovesBackInTheMenu(t *testing.T) {
	m, _ := ompModelDialog(t, []string{"alpha/one", "beta/two", "beta/three"})
	m = typeText(m, "beta")
	m = pressCmd(pressCmd(pressCmd(m, keyCtrlN), keyCtrlN), keyCtrlP)
	line := ""
	for _, row := range strings.Split(screen(m), "\n") {
		if strings.Contains(row, "▸") {
			line = row
		}
	}
	if !strings.Contains(line, "beta/two") || strings.Contains(line, "beta/three") {
		t.Fatalf("ctrl-p mark %q", line)
	}
}

func TestOmpCatalogArrivesAfterTheDialogOpens(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	m = press(m, "n")
	m = typeText(m, "add search")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	m = pressCmd(pressCmd(m, keyLeft), keyTab)
	m = typeText(m, "beta")
	if strings.Contains(screen(m), "beta/two") {
		t.Fatal("menu appeared before the catalog")
	}
	m = update(m, tui.ModelsMsg{Choices: map[domain.Harness][]string{domain.HarnessOmp: {"beta/two", "beta/three"}}})
	if out := screen(m); !strings.Contains(out, "beta/two") {
		t.Fatalf("catalog:\n%s", out)
	}
}

func TestHarnessRowKeepsOmpVisibleAtEightyColumns(t *testing.T) {
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 40})
	m = update(m, tui.StateMsg(withWorkspaces(fixture(1, 0))))
	m = press(m, "n")
	m = pressCmd(pressCmd(m, keyTab), keyTab)
	m = pressCmd(pressCmd(m, keyRight), keyRight)
	out := screen(m)
	if !strings.Contains(out, "● omp") || strings.Contains(out, "○ o…") {
		t.Fatalf("harness row:\n%s", out)
	}
}

func TestModelSwitchMOnOmpFiltersAsYouType(t *testing.T) {
	st := ompState()
	sw := &fakeSwitcher{}
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Switch: sw,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: {"alpha/one", "beta/two"}},
	})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	out := screen(press(m, "M", "b"))
	if !strings.Contains(out, "beta/two") || strings.Contains(out, "alpha/one") {
		t.Fatalf("filter:\n%s", out)
	}
	next, cmd := press(m, "M", "b").Update(keyEnter)
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 model beta/two" {
		t.Fatalf("calls %q", sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "MODEL") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestModelSwitchMOnOmpAppliesAnUnlistedModel(t *testing.T) {
	st := ompState()
	sw := &fakeSwitcher{}
	m := tui.New(tui.Options{
		Theme: tui.Latte(), Now: clock, Switch: sw,
		ModelChoices: map[domain.Harness][]string{domain.HarnessOmp: {"alpha/one"}},
	})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	next, cmd := press(m, "M", "z", "z", "z").Update(keyEnter)
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if len(sw.calls) != 1 || sw.calls[0] != "s01 model zzz" {
		t.Fatalf("calls %q", sw.calls)
	}
	if out := screen(next.(tui.Model)); strings.Contains(out, "MODEL") {
		t.Fatalf("picker still open:\n%s", out)
	}
}

func TestModelSwitchDefaultsReadOmpFromConfig(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	if err := os.WriteFile(path, []byte("[defaults.omp]\nmodel = \"anthropic/opus\"\neffort = \"max\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := tui.LoadDefaults(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := d[domain.HarnessOmp]; got.Model != "anthropic/opus" || got.Effort != "max" {
		t.Fatalf("omp %+v", got)
	}
}
