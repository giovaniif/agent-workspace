package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOnboardStepsFollowThePickedHarnessesInCatalogOrder(t *testing.T) {
	cases := []struct {
		name   string
		picked []Harness
		want   []OnboardStep
	}{
		{"all three", []Harness{HarnessOmp, HarnessCodex, HarnessClaude}, []OnboardStep{OnboardPick, "claude", "codex", "omp", OnboardNvim, OnboardFinish}},
		{"claude only", []Harness{HarnessClaude}, []OnboardStep{OnboardPick, "claude", OnboardNvim, OnboardFinish}},
		{"omp only", []Harness{HarnessOmp}, []OnboardStep{OnboardPick, "omp", OnboardNvim, OnboardFinish}},
		{"none", nil, []OnboardStep{OnboardPick, OnboardNvim, OnboardFinish}},
		{"unknown harness", []Harness{"other"}, []OnboardStep{OnboardPick, OnboardNvim, OnboardFinish}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OnboardSteps(c.picked); !reflect.DeepEqual(got, c.want) {
				t.Errorf("steps = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOnboardStepNamesItsHarness(t *testing.T) {
	cases := []struct {
		step OnboardStep
		want Harness
		ok   bool
	}{
		{HarnessStep(HarnessOmp), HarnessOmp, true},
		{HarnessStep(HarnessCodex), HarnessCodex, true},
		{OnboardNvim, "", false},
		{OnboardPick, "", false},
		{"other", "", false},
	}
	for _, c := range cases {
		if got, ok := c.step.Harness(); got != c.want || ok != c.ok {
			t.Errorf("%s.Harness() = %q, %v; want %q, %v", c.step, got, ok, c.want, c.ok)
		}
	}
}

func TestNextOnboardStepWalksForwardAndStopsAtFinish(t *testing.T) {
	steps := OnboardSteps([]Harness{HarnessOmp})
	cases := []struct {
		cur, want OnboardStep
	}{
		{OnboardPick, "omp"},
		{"omp", OnboardNvim},
		{OnboardNvim, OnboardFinish},
		{OnboardFinish, OnboardFinish},
		{"claude", OnboardFinish},
	}
	for _, c := range cases {
		if got := NextOnboardStep(steps, c.cur); got != c.want {
			t.Errorf("after %s = %s, want %s", c.cur, got, c.want)
		}
	}
}

func TestDefaultOnboardPicksPreferWhatIsAlreadyInstalled(t *testing.T) {
	on := HarnessSetup{Installed: true}
	cases := []struct {
		name string
		o    Onboarding
		want []Harness
	}{
		{"nothing installed picks claude", Onboarding{}, []Harness{HarnessClaude}},
		{"codex installed", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessCodex: on, HarnessClaude: {}}}, []Harness{HarnessCodex}},
		{"omp and claude installed", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessOmp: on, HarnessClaude: on}}, []Harness{HarnessClaude, HarnessOmp}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DefaultOnboardPicks(c.o); !reflect.DeepEqual(got, c.want) {
				t.Errorf("picks = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOnboardingReadsTheOldClaudeAndCodexKeys(t *testing.T) {
	var o Onboarding
	old := `{"done":false,"claude":{"installed":true,"file":"/c/settings.json"},"codex":{"installed":false,"file":"/x/hooks.json"},"nvim":{"on_path":false}}`
	if err := json.Unmarshal([]byte(old), &o); err != nil {
		t.Fatal(err)
	}
	want := map[Harness]HarnessSetup{
		HarnessClaude: {Installed: true, File: "/c/settings.json"},
		HarnessCodex:  {File: "/x/hooks.json"},
	}
	if !reflect.DeepEqual(o.Harnesses, want) {
		t.Fatalf("harnesses = %+v, want %+v", o.Harnesses, want)
	}
	if OnboardingNeeded(o) {
		t.Fatal("an onboarded file with the old keys reopens the walkthrough")
	}
}

func TestOnboardingRoundTripsEveryHarness(t *testing.T) {
	in := Onboarding{Done: true, Harnesses: map[Harness]HarnessSetup{
		HarnessClaude: {Installed: true, File: "/c"},
		HarnessOmp:    {Err: "boom", File: "/o/agentws.ts"},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Onboarding
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("%s decoded to %+v", b, out)
	}
}

func TestHarnessOfferSaysWhatTheStepCanDo(t *testing.T) {
	cases := []struct {
		name string
		h    HarnessSetup
		want SetupOffer
	}{
		{"not installed", HarnessSetup{File: "/c/settings.json"}, OfferInstall},
		{"installed", HarnessSetup{Installed: true, File: "/c/settings.json"}, OfferInstalled},
		{"unreadable config", HarnessSetup{File: "/c/settings.json", Err: "not valid JSON"}, OfferBroken},
		{"error wins over installed", HarnessSetup{Installed: true, Err: "boom"}, OfferBroken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HarnessOffer(c.h); got != c.want {
				t.Errorf("offer = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNvimOfferForEachSetup(t *testing.T) {
	cases := []struct {
		name string
		n    NvimSetup
		want NvimOffer
	}{
		{"no nvim", NvimSetup{PluginFound: true}, NvimMissing},
		{"configured", NvimSetup{OnPath: true, Configured: true, PluginFound: true}, NvimReady},
		{"configured without the shipped dir", NvimSetup{OnPath: true, Configured: true}, NvimReady},
		{"plugin shipped, not configured", NvimSetup{OnPath: true, PluginFound: true}, NvimShowSnippet},
		{"plugin dir missing", NvimSetup{OnPath: true}, NvimNoPlugin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NvimOfferFor(c.n); got != c.want {
				t.Errorf("offer = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNvimSnippetPointsAtThePluginDir(t *testing.T) {
	lua := NvimSnippet("/Users/me/.local/share/agentws/nvim", "/Users/me/.config/nvim/init.lua")
	want := []string{
		"vim.opt.runtimepath:prepend('/Users/me/.local/share/agentws/nvim')",
		"require('agentws').setup({})",
	}
	if !reflect.DeepEqual(lua, want) {
		t.Errorf("lua snippet = %q, want %q", lua, want)
	}

	vim := NvimSnippet("/opt/agentws/nvim", "/home/me/.config/nvim/init.vim")
	if len(vim) != 4 || vim[0] != "lua << EOF" || vim[3] != "EOF" || vim[1] != "vim.opt.runtimepath:prepend('/opt/agentws/nvim')" {
		t.Errorf("vimscript snippet = %q", vim)
	}

	quoted := strings.Join(NvimSnippet("/tmp/it's here", "init.lua"), "\n")
	if !strings.Contains(quoted, `prepend('/tmp/it\'s here')`) {
		t.Errorf("a quote in the path is not escaped: %q", quoted)
	}
}
