// why: it reads config files and looks up nvim, so the daemon calls it on a connection goroutine, never on its loop.
package onboard

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/adapters/omp"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

// why: the marker lives in AGENTWS_HOME, so a temp home in tests or make dev starts fresh.
const doneFile = "onboarded"

// why: a large nvim config tree must not stall the walkthrough; agentws is set up near the top of it.
const maxConfigFiles = 400

var mentionsPlugin = regexp.MustCompile(`require\s*\(?\s*['"]agentws['"]|agentws/nvim`)

type Probe struct {
	Home           string
	Bin            string
	ClaudeSettings string
	CodexHome      string
	OmpAgentDir    string
	// why: an empty HOME would otherwise install the hook under a relative .omp/agent.
	ompDirErr     error
	NvimConfigDir string
	// why: the first existing dir wins, so a release install beats a source checkout.
	PluginDirs []string
	LookPath   func(string) (string, error)
}

// why: a source checkout's nvim/ next to its bin/ is the last plugin candidate, so a make build finds it too.
func FromEnv(home, bin string, env func(string) string) Probe {
	user := env("HOME")
	or := func(v, fallback string) string {
		if v != "" {
			return v
		}
		return fallback
	}
	ompDir, ompErr := omp.AgentDir(env)
	claudeDir := or(env("CLAUDE_CONFIG_DIR"), filepath.Join(user, ".claude"))
	data := or(env("XDG_DATA_HOME"), filepath.Join(user, ".local", "share"))
	return Probe{
		Home:           home,
		Bin:            bin,
		ClaudeSettings: filepath.Join(claudeDir, "settings.json"),
		CodexHome:      or(env("CODEX_HOME"), filepath.Join(user, ".codex")),
		OmpAgentDir:    ompDir,
		ompDirErr:      ompErr,
		NvimConfigDir:  filepath.Join(or(env("XDG_CONFIG_HOME"), filepath.Join(user, ".config")), "nvim"),
		PluginDirs: []string{
			filepath.Join(data, "agentws", "nvim"),
			filepath.Join(filepath.Dir(filepath.Dir(bin)), "nvim"),
		},
		LookPath: exec.LookPath,
	}
}

func (p Probe) Onboarding(context.Context) (domain.Onboarding, error) {
	_, err := os.Stat(filepath.Join(p.Home, doneFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return domain.Onboarding{}, err
	}
	return domain.Onboarding{
		Done: err == nil,
		Harnesses: map[domain.Harness]domain.HarnessSetup{
			domain.HarnessClaude: p.claudeSetup(),
			domain.HarnessCodex:  p.codexSetup(),
			domain.HarnessOmp:    p.ompSetup(),
		},
		Nvim: p.nvimSetup(),
	}, nil
}

func (p Probe) Install(_ context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	switch h {
	case domain.HarnessClaude:
		existed := exists(p.ClaudeSettings)
		if err := claude.Setup(p.ClaudeSettings, p.Bin); err != nil {
			return domain.HarnessSetup{}, err
		}
		s := p.claudeSetup()
		if !existed {
			s.Backup = ""
		}
		return s, nil
	case domain.HarnessCodex:
		res, err := codex.Setup(p.codexConfig())
		if err != nil {
			return domain.HarnessSetup{}, err
		}
		s := p.codexSetup()
		s.Backup = res.Backup
		return s, nil
	case domain.HarnessOmp:
		if p.ompDirErr != nil {
			return domain.HarnessSetup{}, p.ompDirErr
		}
		if _, err := omp.Setup(p.ompConfig()); err != nil {
			return domain.HarnessSetup{}, err
		}
		return p.ompSetup(), nil
	}
	return domain.HarnessSetup{}, errors.New("no setup for harness " + string(h))
}

func (p Probe) Finish(context.Context) error {
	if err := os.MkdirAll(p.Home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.Home, doneFile), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func (p Probe) claudeSetup() domain.HarnessSetup {
	s := domain.HarnessSetup{File: p.ClaudeSettings}
	if exists(p.ClaudeSettings) {
		s.Backup = claude.BackupPath(p.ClaudeSettings)
	}
	ok, err := claude.Installed(p.ClaudeSettings, p.Bin)
	s.Installed = ok
	if err != nil {
		s.Err = err.Error()
	}
	return s
}

func (p Probe) codexConfig() codex.SetupConfig {
	return codex.SetupConfig{Dir: p.CodexHome, Command: p.Bin}
}

func (p Probe) codexSetup() domain.HarnessSetup {
	file := filepath.Join(p.CodexHome, "hooks.json")
	s := domain.HarnessSetup{File: file}
	if exists(file) {
		s.Backup = file + ".agentws-<time>.bak"
	}
	ok, err := codex.Installed(p.codexConfig())
	s.Installed = ok
	if err != nil {
		s.Err = err.Error()
	}
	return s
}

func (p Probe) ompConfig() omp.SetupConfig {
	return omp.SetupConfig{Dir: p.OmpAgentDir, Command: p.Bin}
}

func (p Probe) ompSetup() domain.HarnessSetup {
	if p.ompDirErr != nil {
		return domain.HarnessSetup{Err: p.ompDirErr.Error()}
	}
	s := domain.HarnessSetup{File: omp.HookFile(p.OmpAgentDir)}
	ok, err := omp.Installed(p.ompConfig())
	s.Installed = ok
	if err != nil {
		s.Err = err.Error()
	}
	return s
}

func (p Probe) nvimSetup() domain.NvimSetup {
	var n domain.NvimSetup
	if p.LookPath != nil {
		_, err := p.LookPath("nvim")
		n.OnPath = err == nil
	}
	n.ConfigFile = p.nvimSetupFile()
	n.Configured = p.configMentionsPlugin()
	for _, dir := range p.PluginDirs {
		if isDir(dir) {
			n.PluginDir, n.PluginFound = dir, true
			break
		}
	}
	if !n.PluginFound && len(p.PluginDirs) > 0 {
		n.PluginDir = p.PluginDirs[0]
	}
	return n
}

func (p Probe) configMentionsPlugin() bool {
	seen := 0
	found := false
	root := p.NvimConfigDir
	// why: WalkDir does not follow a symlinked root, and ~/.config/nvim often is one.
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".lua" && ext != ".vim" {
			return nil
		}
		if seen++; seen > maxConfigFiles {
			return filepath.SkipAll
		}
		b, err := os.ReadFile(path)
		if err == nil && mentionsPlugin.Match(b) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
