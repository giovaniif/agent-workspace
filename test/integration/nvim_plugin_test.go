//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pluginDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "nvim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "lua", "crew", "init.lua")); err != nil {
		t.Fatalf("the nvim plugin is missing: %v", err)
	}
	return dir
}

func TestNvimPluginSpecs(t *testing.T) {
	plugin := pluginDir(t)
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	for _, spec := range []string{"spec.lua", "legacy_spec.lua"} {
		t.Run(spec, func(t *testing.T) {
			home := t.TempDir()
			cmd := exec.Command("nvim", "--clean", "--headless", "-n", "-i", "NONE", "-l", filepath.Join(plugin, "test", spec))
			cmd.Env = append(os.Environ(),
				"CREW_NVIM_PLUGIN="+plugin,
				"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
				"XDG_DATA_HOME="+filepath.Join(home, "data"),
				"XDG_STATE_HOME="+filepath.Join(home, "state"),
				"XDG_CACHE_HOME="+filepath.Join(home, "cache"),
				"CREW_SESSION=",
				legacySessionEnv,
				"TMUX=",
			)
			out, err := cmd.CombinedOutput()
			t.Logf("%s", out)
			if err != nil {
				t.Fatalf("nvim specs failed: %v", err)
			}
			if !strings.Contains(string(out), "all specs passed") {
				t.Fatalf("the specs did not finish:\n%s", out)
			}
		})
	}
}
