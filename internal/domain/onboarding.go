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

type NvimSetup struct {
	OnPath      bool   `json:"on_path"`
	Configured  bool   `json:"configured"`
	ConfigFile  string `json:"config_file"`
	PluginDir   string `json:"plugin_dir"`
	PluginFound bool   `json:"plugin_found"`
}

func OnboardingNeeded(o Onboarding) bool {
	if o.Done {
		return false
	}
	harness := len(installedHarnesses(o)) > 0
	nvimDone := !o.Nvim.OnPath || o.Nvim.Configured
	return !harness || !nvimDone
}

const nvimSetupMarker = "-- crew: written by crew setup nvim; crew setup nvim --remove deletes this file."

func NvimSetupFile(pluginDir string) string {
	return strings.Join([]string{
		nvimSetupMarker,
		"local dir = '" + luaQuote(pluginDir) + "'",
		"vim.opt.runtimepath:prepend(dir)",
		"dofile(dir .. '/plugin/crew.lua')",
		"require('crew').setup({})",
	}, "\n") + "\n"
}

func IsNvimSetupFile(content string) bool { return strings.HasPrefix(content, nvimSetupMarker) }

func luaQuote(s string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) }

type Onboarding struct {
	Done      bool                     `json:"done"`
	Harnesses map[Harness]HarnessSetup `json:"harnesses"`
	Nvim      NvimSetup                `json:"nvim"`
}

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

const CodexTrustStep = "Codex runs a hook only after you trust it. Start codex and accept the review prompt for the new crew hooks, or open /hooks and trust them there."

func OnboardSteps(picked []Harness) []OnboardStep {
	steps := []OnboardStep{OnboardPick}
	for _, h := range Harnesses() {
		if slices.Contains(picked, h) {
			steps = append(steps, HarnessStep(h))
		}
	}
	return append(steps, OnboardNvim, OnboardFinish)
}

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

func NvimSnippet(pluginDir, configFile string) []string {
	quoted := luaQuote(pluginDir)
	lua := []string{
		"vim.opt.runtimepath:prepend('" + quoted + "')",
		"require('crew').setup({})",
	}
	if strings.HasSuffix(configFile, ".vim") {
		return append(append([]string{"lua << EOF"}, lua...), "EOF")
	}
	return lua
}
