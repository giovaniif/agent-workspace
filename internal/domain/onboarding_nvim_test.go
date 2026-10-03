package domain

import (
	"strings"
	"testing"
)

func TestOnboardingNeededOnlyWhenSomethingIsLeft(t *testing.T) {
	on := HarnessSetup{Installed: true}
	cases := []struct {
		name string
		o    Onboarding
		want bool
	}{
		{"fresh machine", Onboarding{Nvim: NvimSetup{OnPath: true}}, true},
		{"done", Onboarding{Done: true}, false},
		{"claude set up, no nvim", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessClaude: on}}, false},
		{"omp set up, no nvim", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessOmp: on, HarnessClaude: {}}}, false},
		{"codex set up, nvim configured", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessCodex: on}, Nvim: NvimSetup{OnPath: true, Configured: true}}, false},
		{"claude set up, nvim not configured", Onboarding{Harnesses: map[Harness]HarnessSetup{HarnessClaude: on}, Nvim: NvimSetup{OnPath: true}}, true},
		{"no harness, nvim configured", Onboarding{Nvim: NvimSetup{OnPath: true, Configured: true}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OnboardingNeeded(c.o); got != c.want {
				t.Errorf("needed = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNvimSetupFileLoadsThePluginAndIsRecognised(t *testing.T) {
	got := NvimSetupFile("/p/it's/nvim")
	for _, want := range []string{
		"local dir = '/p/it\\'s/nvim'",
		"vim.opt.runtimepath:prepend(dir)",
		"dofile(dir .. '/plugin/agentws.lua')",
		"require('agentws').setup({})",
		"agentws setup nvim --remove",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if !IsNvimSetupFile(got) {
		t.Error("the written file is not recognised as ours")
	}
	if IsNvimSetupFile("require('agentws').setup({})\n") {
		t.Error("a user's own file counts as ours")
	}
}
