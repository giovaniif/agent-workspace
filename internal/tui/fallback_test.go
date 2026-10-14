package tui_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func fallbackDialog(t *testing.T, st rpc.State, opts tui.Options) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	opts.Theme, opts.Now, opts.Calls = tui.Latte(), clock, c
	m := tui.New(opts)
	m = update(m, tea.WindowSizeMsg{Width: 60, Height: 40})
	m = update(m, tui.StateMsg(st))
	return typeText(press(m, "n"), "fix the login bug"), c
}

func TestFallbackLoadReadsThresholdAndMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[fallback]\nthreshold = 40\n[fallback.models]\nopus = \"gpt-5\"\n[fallback.efforts]\nmax = \"medium\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := tui.LoadFallback(path)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.FallbackConfig{Threshold: 40, Models: map[string]string{"opus": "gpt-5"}, Efforts: map[string]string{"max": "medium"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	none, err := tui.LoadFallback(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || !reflect.DeepEqual(none, domain.FallbackConfig{}) {
		t.Fatalf("missing file gave %+v, %v", none, err)
	}
}

func TestFallbackDialogOffersCodexWithTheMappedModelAndEffort(t *testing.T) {
	opts := tui.Options{
		Defaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {Model: "opus", Effort: "high"}},
		Fallback: domain.FallbackConfig{Models: map[string]string{"opus": "gpt-5"}, Efforts: map[string]string{"high": "medium"}},
	}
	m, c := fallbackDialog(t, lowClaudeState(), opts)
	if out := screen(m); !strings.Contains(out, "Codex 5h is 36% used · starts as gpt-5") {
		t.Fatalf("offer does not name the mapped model:\n%s", out)
	}
	m = pressCmd(m, keyCtrlS)
	if out := screen(m); !strings.Contains(out, "● codex") || !strings.Contains(out, "gpt-5") || !strings.Contains(out, "‹ medium ›") {
		t.Fatalf("ctrl+s did not carry the mapping over:\n%s", out)
	}
	got := startedParams(t, m, c)
	if got.Harness != "codex" || got.Model != "gpt-5" || got.Effort != "medium" {
		t.Fatalf("params %+v", got)
	}
}

func TestFallbackDialogKeepsCodexDefaultsWhereTheMappingIsEmpty(t *testing.T) {
	opts := tui.Options{
		Defaults: map[domain.Harness]tui.Defaults{
			domain.HarnessClaude: {Model: "haiku"},
			domain.HarnessCodex:  {Model: "gpt-5-codex", Effort: "medium"},
		},
	}
	m, c := fallbackDialog(t, lowClaudeState(), opts)
	m = pressCmd(m, keyCtrlS)
	got := startedParams(t, m, c)
	if got.Model != "gpt-5-codex" || got.Effort != "medium" {
		t.Fatalf("params %+v", got)
	}
}

func TestFallbackDialogHonoursTheConfiguredThreshold(t *testing.T) {
	st := lowClaudeState()
	st.Sessions[0].Limits[0].UsedPercent = 70
	m, _ := fallbackDialog(t, st, tui.Options{})
	if out := screen(m); strings.Contains(out, "ctrl+s") {
		t.Fatalf("offered at 30%% with the default threshold:\n%s", out)
	}
	m, _ = fallbackDialog(t, st, tui.Options{Fallback: domain.FallbackConfig{Threshold: 40}})
	if out := screen(m); !strings.Contains(out, "Claude 5h window 70% used") || !strings.Contains(out, "ctrl+s start in codex") {
		t.Fatalf("no offer at 30%% with a threshold of 40:\n%s", out)
	}
}

func TestFallbackDialogRestoresTheClaudeStartWhenTheUserGoesBack(t *testing.T) {
	opts := tui.Options{
		Defaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {Model: "opus", Effort: "high"}},
		Fallback: domain.FallbackConfig{Models: map[string]string{"opus": "gpt-5"}, Efforts: map[string]string{"high": "medium"}},
	}
	m, c := fallbackDialog(t, lowClaudeState(), opts)
	m = pressCmd(m, keyCtrlS)
	m = pressCmd(pressCmd(pressCmd(m, keyTab), keyTab), keyLeft)
	got := startedParams(t, m, c)
	if got.Harness != "claude" || got.Model != "opus" || got.Effort != "high" {
		t.Fatalf("params %+v", got)
	}
}

func TestFallbackDialogKeepsAMappedEffortThePickerDoesNotList(t *testing.T) {
	opts := tui.Options{
		Defaults: map[domain.Harness]tui.Defaults{domain.HarnessClaude: {Effort: "high"}},
		Fallback: domain.FallbackConfig{Efforts: map[string]string{"high": "xhigh"}},
	}
	m, c := fallbackDialog(t, lowClaudeState(), opts)
	m = pressCmd(m, keyCtrlS)
	if out := screen(m); !strings.Contains(out, "‹ xhigh ›") {
		t.Fatalf("mapped effort not shown:\n%s", out)
	}
	if got := startedParams(t, m, c); got.Effort != "xhigh" {
		t.Fatalf("params %+v", got)
	}
}

func TestFallbackDialogDoesNotOfferAnExhaustedCodex(t *testing.T) {
	st := lowClaudeState()
	st.Sessions[1].Limits[0].UsedPercent = 97
	m, _ := fallbackDialog(t, st, tui.Options{})
	out := screen(m)
	if !strings.Contains(out, "Claude 5h window 90% used") || strings.Contains(out, "ctrl+s") {
		t.Fatalf("warning should carry no offer:\n%s", out)
	}
}
