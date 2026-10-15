package onboard_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnOldNvimRequireStillCountsAsConfigured(t *testing.T) {
	cases := map[string]map[string]string{
		"init.lua requires the old module": {"init.lua": "require('agentws').setup({})"},
		"a lua module requires it":         {"init.lua": "require('me')", "lua/me/plugins.lua": `require("agentws").setup{}`},
		"a plugin manager names the dir":   {"init.lua": "{ dir = '~/.local/share/agentws/nvim' }"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, true)
			if err := os.MkdirAll(w.probe.PluginDirs[1], 0o755); err != nil {
				t.Fatal(err)
			}
			for file, body := range files {
				write(t, filepath.Join(w.probe.NvimConfigDir, file), body)
			}
			got, err := w.probe.Onboarding(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !got.Nvim.Configured {
				t.Errorf("nvim = %+v, want configured", got.Nvim)
			}
		})
	}
}
