package onboard_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/onboard"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

const bin = "/opt/agentws/bin/agentws"

type world struct {
	probe onboard.Probe
	root  string
}

func newWorld(t *testing.T, nvimOnPath bool) world {
	t.Helper()
	root := t.TempDir()
	look := func(name string) (string, error) {
		if name == "nvim" && nvimOnPath {
			return "/usr/bin/nvim", nil
		}
		return "", errors.New("not found")
	}
	return world{root: root, probe: onboard.Probe{
		Home:           filepath.Join(root, "agentws"),
		Bin:            bin,
		ClaudeSettings: filepath.Join(root, "claude", "settings.json"),
		CodexHome:      filepath.Join(root, "codex"),
		OmpAgentDir:    filepath.Join(root, "omp", "agent"),
		NvimConfigDir:  filepath.Join(root, "config", "nvim"),
		PluginDirs:     []string{filepath.Join(root, "share", "agentws", "nvim"), filepath.Join(root, "repo", "nvim")},
		LookPath:       look,
	}}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAFreshMachineHasNothingSetUp(t *testing.T) {
	w := newWorld(t, false)
	got, err := w.probe.Onboarding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Done || got.Harnesses[domain.HarnessClaude].Installed || got.Harnesses[domain.HarnessCodex].Installed || got.Nvim.OnPath || got.Nvim.Configured {
		t.Errorf("fresh machine = %+v", got)
	}
	if got.Harnesses[domain.HarnessClaude].File != w.probe.ClaudeSettings || got.Harnesses[domain.HarnessCodex].File != filepath.Join(w.probe.CodexHome, "hooks.json") {
		t.Errorf("files = %q, %q", got.Harnesses[domain.HarnessClaude].File, got.Harnesses[domain.HarnessCodex].File)
	}
	if got.Harnesses[domain.HarnessClaude].Backup != "" || got.Harnesses[domain.HarnessCodex].Backup != "" {
		t.Errorf("backups named for files that do not exist: %q, %q", got.Harnesses[domain.HarnessClaude].Backup, got.Harnesses[domain.HarnessCodex].Backup)
	}
	if got.Nvim.ConfigFile != filepath.Join(w.probe.NvimConfigDir, "plugin", "agentws.lua") {
		t.Errorf("nvim setup file = %q, want plugin/agentws.lua under the config dir", got.Nvim.ConfigFile)
	}
	if got.Nvim.PluginFound || got.Nvim.PluginDir != w.probe.PluginDirs[0] {
		t.Errorf("plugin = %q found %v, want the first candidate, not found", got.Nvim.PluginDir, got.Nvim.PluginFound)
	}
}

func TestInstallSetsUpEachHarnessAndOnboardingSeesIt(t *testing.T) {
	w := newWorld(t, false)
	write(t, w.probe.ClaudeSettings, `{"theme":"dark"}`)
	ctx := context.Background()

	claude, err := w.probe.Install(ctx, domain.HarnessClaude)
	if err != nil || !claude.Installed {
		t.Fatalf("install claude = %+v, %v", claude, err)
	}
	if _, err := os.Stat(claude.Backup); err != nil {
		t.Errorf("claude backup %q: %v", claude.Backup, err)
	}
	codex, err := w.probe.Install(ctx, domain.HarnessCodex)
	if err != nil || !codex.Installed {
		t.Fatalf("install codex = %+v, %v", codex, err)
	}
	b, err := os.ReadFile(filepath.Join(w.probe.CodexHome, "hooks.json"))
	if err != nil || !strings.Contains(string(b), bin+" hook --harness codex") {
		t.Errorf("hooks.json = %s, %v", b, err)
	}

	got, err := w.probe.Onboarding(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Harnesses[domain.HarnessClaude].Installed || !got.Harnesses[domain.HarnessCodex].Installed {
		t.Errorf("after install = %+v", got)
	}
	again, err := w.probe.Install(ctx, domain.HarnessClaude)
	if err != nil || !again.Installed {
		t.Errorf("second install = %+v, %v", again, err)
	}
}

func TestABrokenConfigIsReportedNotOverwritten(t *testing.T) {
	w := newWorld(t, false)
	write(t, w.probe.ClaudeSettings, `{broken`)
	got, err := w.probe.Onboarding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Harnesses[domain.HarnessClaude].Err == "" || got.Harnesses[domain.HarnessClaude].Installed {
		t.Errorf("broken settings = %+v", got.Harnesses[domain.HarnessClaude])
	}
	if _, err := w.probe.Install(context.Background(), domain.HarnessClaude); err == nil {
		t.Error("install over broken JSON succeeded")
	}
	if b, _ := os.ReadFile(w.probe.ClaudeSettings); string(b) != `{broken` {
		t.Errorf("settings rewritten: %s", b)
	}
}

func TestNvimPluginIsFoundAndItsConfigurationDetected(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string
		configured bool
		configFile string
	}{
		{"no config", nil, false, "plugin/agentws.lua"},
		{"init.lua without agentws", map[string]string{"init.lua": "vim.o.number = true"}, false, "plugin/agentws.lua"},
		{"init.lua requires it", map[string]string{"init.lua": "require('agentws').setup({})"}, true, "plugin/agentws.lua"},
		{"a lua module requires it", map[string]string{"init.lua": "require('me')", "lua/me/plugins.lua": `require("agentws").setup{}`}, true, "plugin/agentws.lua"},
		{"init.vim only", map[string]string{"init.vim": "set number"}, false, "plugin/agentws.lua"},
		{"lazy.nvim spec in lua/plugins", map[string]string{"init.lua": "require('config.lazy')", "lua/plugins/agentws.lua": "return { dir = vim.fn.expand('~/.local/share/agentws/nvim'), config = function() require('agentws').setup({}) end }"}, true, "plugin/agentws.lua"},
		{"plugin manager spec names the dir", map[string]string{"init.lua": "{ dir = '~/.local/share/agentws/nvim' }"}, true, "plugin/agentws.lua"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, true)
			if err := os.MkdirAll(w.probe.PluginDirs[1], 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range c.files {
				write(t, filepath.Join(w.probe.NvimConfigDir, name), body)
			}
			got, err := w.probe.Onboarding(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			n := got.Nvim
			if !n.OnPath || n.Configured != c.configured {
				t.Errorf("nvim = %+v, want configured %v", n, c.configured)
			}
			if n.ConfigFile != filepath.Join(w.probe.NvimConfigDir, c.configFile) {
				t.Errorf("config file = %q, want %s", n.ConfigFile, c.configFile)
			}
			if !n.PluginFound || n.PluginDir != w.probe.PluginDirs[1] {
				t.Errorf("plugin = %q found %v, want the existing candidate", n.PluginDir, n.PluginFound)
			}
		})
	}
}

func TestFinishRecordsCompletionInHome(t *testing.T) {
	w := newWorld(t, false)
	ctx := context.Background()
	if err := w.probe.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := w.probe.Onboarding(ctx)
	if err != nil || !got.Done {
		t.Errorf("after finish = %+v, %v", got, err)
	}
	fresh := newWorld(t, false)
	if got, _ := fresh.probe.Onboarding(ctx); got.Done {
		t.Error("another home counts as done")
	}
}

func TestOmpIsDetectedAndInstalledThroughItsHookFile(t *testing.T) {
	w := newWorld(t, false)
	ctx := context.Background()
	file := filepath.Join(w.root, "omp", "agent", "hooks", "post", "agentws.ts")
	fresh, err := w.probe.Onboarding(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.Harnesses[domain.HarnessOmp]; got != (domain.HarnessSetup{File: file}) {
		t.Errorf("fresh omp = %+v", got)
	}
	installed, err := w.probe.Install(ctx, domain.HarnessOmp)
	if err != nil || installed != (domain.HarnessSetup{Installed: true, File: file}) {
		t.Fatalf("install omp = %+v, %v", installed, err)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), `const agentws = "`+bin+`";`) {
		t.Errorf("hook file = %s, %v", b, err)
	}
	if got, _ := w.probe.Onboarding(ctx); !got.Harnesses[domain.HarnessOmp].Installed {
		t.Errorf("after install = %+v", got.Harnesses[domain.HarnessOmp])
	}
}

func TestAnOmpHookFileOfTheUsersIsReportedNotOverwritten(t *testing.T) {
	w := newWorld(t, false)
	file := filepath.Join(w.root, "omp", "agent", "hooks", "post", "agentws.ts")
	write(t, file, "export default function () {}\n")
	got, err := w.probe.Onboarding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Harnesses[domain.HarnessOmp]; s.Err == "" || s.Installed {
		t.Errorf("omp = %+v", s)
	}
	if _, err := w.probe.Install(context.Background(), domain.HarnessOmp); err == nil {
		t.Error("install over the user's file succeeded")
	}
	if b, _ := os.ReadFile(file); string(b) != "export default function () {}\n" {
		t.Errorf("the user's file became %q", b)
	}
}

func TestFromEnvReportsAnUnknownOmpHome(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p := onboard.FromEnv("/h", bin, func(string) string { return "" })
	got, err := p.Onboarding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Harnesses[domain.HarnessOmp]; s.Err == "" || s.Installed || s.File != "" {
		t.Fatalf("omp = %+v", s)
	}
	if _, err := p.Install(context.Background(), domain.HarnessOmp); err == nil {
		t.Fatal("install wrote without a home")
	}
	if _, err := os.Stat(filepath.Join(dir, ".omp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("relative .omp created: %v", err)
	}
}

func TestFromEnvFollowsTheHarnessAndXDGVariables(t *testing.T) {
	env := map[string]string{
		"CLAUDE_CONFIG_DIR":   "/c",
		"CODEX_HOME":          "/x",
		"PI_CODING_AGENT_DIR": "/pi",
		"XDG_CONFIG_HOME":     "/cfg",
		"XDG_DATA_HOME":       "/data",
		"HOME":                "/home/me",
	}
	p := onboard.FromEnv("/h", "/repo/bin/agentws", func(k string) string { return env[k] })
	if p.ClaudeSettings != "/c/settings.json" || p.CodexHome != "/x" || p.OmpAgentDir != "/pi" || p.NvimConfigDir != "/cfg/nvim" || p.Home != "/h" {
		t.Errorf("probe = %+v", p)
	}
	if len(p.PluginDirs) < 2 || p.PluginDirs[0] != "/data/agentws/nvim" || p.PluginDirs[len(p.PluginDirs)-1] != "/repo/nvim" {
		t.Errorf("plugin dirs = %q, want the data dir first and the checkout's nvim/ last", p.PluginDirs)
	}

	bare := onboard.FromEnv("/h", "/usr/local/bin/agentws", func(k string) string {
		if k == "HOME" {
			return "/home/me"
		}
		return ""
	})
	if bare.ClaudeSettings != "/home/me/.claude/settings.json" || bare.CodexHome != "/home/me/.codex" || bare.OmpAgentDir != "/home/me/.omp/agent" || bare.NvimConfigDir != "/home/me/.config/nvim" || bare.PluginDirs[0] != "/home/me/.local/share/agentws/nvim" {
		t.Errorf("defaults = %+v", bare)
	}
}

func TestNvimConfigBehindASymlinkIsScanned(t *testing.T) {
	w := newWorld(t, true)
	real := filepath.Join(t.TempDir(), "dotfiles-nvim")
	write(t, filepath.Join(real, "init.lua"), "require('agentws').setup({})")
	if err := os.RemoveAll(w.probe.NvimConfigDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(w.probe.NvimConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, w.probe.NvimConfigDir); err != nil {
		t.Fatal(err)
	}
	got, err := w.probe.Onboarding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Nvim.Configured {
		t.Errorf("nvim = %+v, want configured through the symlink", got.Nvim)
	}
}

func TestRemoveBacksUpTheHookFileFirstAndCanBeReinstalled(t *testing.T) {
	w := newWorld(t, false)
	ctx := context.Background()
	write(t, w.probe.ClaudeSettings, `{"model":"opus"}`)
	for _, h := range []domain.Harness{domain.HarnessClaude, domain.HarnessCodex} {
		if _, err := w.probe.Install(ctx, h); err != nil {
			t.Fatal(err)
		}
		installed, err := os.ReadFile(setupFile(w, h))
		if err != nil {
			t.Fatal(err)
		}
		got, err := w.probe.Remove(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if got.Installed || got.Backup == "" {
			t.Fatalf("%s remove = %+v", h, got)
		}
		saved, err := os.ReadFile(got.Backup)
		if err != nil || string(saved) != string(installed) {
			t.Errorf("%s backup = %q, %v; want the installed file", h, saved, err)
		}
		again, err := w.probe.Install(ctx, h)
		if err != nil || !again.Installed {
			t.Errorf("%s reinstall = %+v, %v", h, again, err)
		}
	}
	after, err := os.ReadFile(w.probe.ClaudeSettings)
	if err != nil || !strings.Contains(string(after), `"opus"`) {
		t.Errorf("claude settings lost the user's keys: %s, %v", after, err)
	}
}

func TestRemoveWithNothingInstalledMakesNoBackup(t *testing.T) {
	w := newWorld(t, false)
	got, err := w.probe.Remove(context.Background(), domain.HarnessClaude)
	if err != nil || got.Installed || got.Backup != "" {
		t.Errorf("remove = %+v, %v", got, err)
	}
	if _, err := os.Stat(w.probe.ClaudeSettings); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("settings.json created: %v", err)
	}
}

func setupFile(w world, h domain.Harness) string {
	if h == domain.HarnessCodex {
		return filepath.Join(w.probe.CodexHome, "hooks.json")
	}
	return w.probe.ClaudeSettings
}
