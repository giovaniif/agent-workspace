package domain

import (
	"encoding/json"
	"slices"
	"strings"
)

type HarnessSetup struct {
	Installed bool   `json:"installed"`
	File      string `json:"file"`
	Backup    string `json:"backup,omitempty"`
	Err       string `json:"err,omitempty"`
}

// why: PluginFound tells a plugin dir the snippet can point at from one a source build lacks.
type NvimSetup struct {
	OnPath      bool   `json:"on_path"`
	Configured  bool   `json:"configured"`
	ConfigFile  string `json:"config_file"`
	PluginDir   string `json:"plugin_dir"`
	PluginFound bool   `json:"plugin_found"`
}

// why: an existing user who set up a harness, and has no nvim left to configure, must not get the walkthrough after upgrading.
func OnboardingNeeded(o Onboarding) bool {
	if o.Done {
		return false
	}
	harness := len(installedHarnesses(o)) > 0
	nvimDone := !o.Nvim.OnPath || o.Nvim.Configured
	return !harness || !nvimDone
}

const nvimSetupMarker = "-- agentws: written by agentws setup nvim; agentws setup nvim --remove deletes this file."

// why: nvim sources every config's plugin/ dir whatever the setup (init.vim, lazy.nvim, kickstart), so a file of its own needs no edit to the user's files.
func NvimSetupFile(pluginDir string) string {
	return strings.Join([]string{
		nvimSetupMarker,
		"local dir = '" + luaQuote(pluginDir) + "'",
		"vim.opt.runtimepath:prepend(dir)",
		"dofile(dir .. '/plugin/agentws.lua')",
		"require('agentws').setup({})",
	}, "\n") + "\n"
}

// why: remove deletes only a file agentws wrote, never one the user put at the same path.
func IsNvimSetupFile(content string) bool { return strings.HasPrefix(content, nvimSetupMarker) }

func luaQuote(s string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) }

type Onboarding struct {
	Done      bool                     `json:"done"`
	Harnesses map[Harness]HarnessSetup `json:"harnesses"`
	Nvim      NvimSetup                `json:"nvim"`
}

// why: a daemon from before omp sends claude and codex as their own keys.
func (o *Onboarding) UnmarshalJSON(b []byte) error {
	type plain Onboarding
	var wire struct {
		plain
		Claude *HarnessSetup `json:"claude"`
		Codex  *HarnessSetup `json:"codex"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	*o = Onboarding(wire.plain)
	for h, setup := range map[Harness]*HarnessSetup{HarnessClaude: wire.Claude, HarnessCodex: wire.Codex} {
		if _, ok := o.Harnesses[h]; setup == nil || ok {
			continue
		}
		if o.Harnesses == nil {
			o.Harnesses = map[Harness]HarnessSetup{}
		}
		o.Harnesses[h] = *setup
	}
	return nil
}

type OnboardStep string

const (
	OnboardPick   OnboardStep = "pick"
	OnboardNvim   OnboardStep = "nvim"
	OnboardFinish OnboardStep = "finish"
)

func HarnessStep(h Harness) OnboardStep { return OnboardStep(h) }

func (s OnboardStep) Harness() (Harness, bool) {
	if h := Harness(s); slices.Contains(Harnesses(), h) {
		return h, true
	}
	return "", false
}

// why: Codex keeps a hash of every hook it was told to trust, so new hooks stay off until the user trusts them.
const CodexTrustStep = "Codex runs a hook only after you trust it. Start codex and accept the review prompt for the new agentws hooks, or open /hooks and trust them there."

func OnboardSteps(picked []Harness) []OnboardStep {
	steps := []OnboardStep{OnboardPick}
	for _, h := range Harnesses() {
		if slices.Contains(picked, h) {
			steps = append(steps, HarnessStep(h))
		}
	}
	return append(steps, OnboardNvim, OnboardFinish)
}

// why: a step not in steps goes to finish, so a stale step never loops.
func NextOnboardStep(steps []OnboardStep, cur OnboardStep) OnboardStep {
	for i, s := range steps {
		if s == cur && i+1 < len(steps) {
			return steps[i+1]
		}
	}
	return OnboardFinish
}

func DefaultOnboardPicks(o Onboarding) []Harness {
	if picks := installedHarnesses(o); len(picks) > 0 {
		return picks
	}
	return []Harness{HarnessClaude}
}

func installedHarnesses(o Onboarding) []Harness {
	var out []Harness
	for _, h := range Harnesses() {
		if o.Harnesses[h].Installed {
			out = append(out, h)
		}
	}
	return out
}

type SetupOffer int

const (
	OfferInstall SetupOffer = iota
	OfferInstalled
	// why: a config agentws cannot read gets no install offer, so it is never overwritten.
	OfferBroken
)

func HarnessOffer(h HarnessSetup) SetupOffer {
	switch {
	case h.Err != "":
		return OfferBroken
	case h.Installed:
		return OfferInstalled
	}
	return OfferInstall
}

type NvimOffer int

const (
	NvimMissing NvimOffer = iota
	NvimReady
	NvimShowSnippet
	// why: a build that did not come from a release archive has no plugin files.
	NvimNoPlugin
)

func NvimOfferFor(n NvimSetup) NvimOffer {
	switch {
	case !n.OnPath:
		return NvimMissing
	case n.Configured:
		return NvimReady
	case n.PluginFound:
		return NvimShowSnippet
	}
	return NvimNoPlugin
}

// why: an init.vim cannot hold lua directly, so it gets a heredoc.
func NvimSnippet(pluginDir, configFile string) []string {
	quoted := luaQuote(pluginDir)
	lua := []string{
		"vim.opt.runtimepath:prepend('" + quoted + "')",
		"require('agentws').setup({})",
	}
	if strings.HasSuffix(configFile, ".vim") {
		return append(append([]string{"lua << EOF"}, lua...), "EOF")
	}
	return lua
}
