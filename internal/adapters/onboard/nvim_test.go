package onboard_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallNvimWritesItsOwnFileForEveryConfigStyle(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"no config dir", nil},
		{"init.lua", map[string]string{"init.lua": "vim.o.number = true\n"}},
		{"init.vim", map[string]string{"init.vim": "set number\n"}},
		{"lazy.nvim", map[string]string{"init.lua": "require('config.lazy')\n", "lua/plugins/ui.lua": "return { 'folke/tokyonight.nvim' }\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, true)
			if err := os.MkdirAll(w.probe.PluginDirs[0], 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range c.files {
				write(t, filepath.Join(w.probe.NvimConfigDir, name), body)
			}
			ctx := context.Background()
			n, err := w.probe.InstallNvim(ctx)
			file := filepath.Join(w.probe.NvimConfigDir, "plugin", "crew.lua")
			if err != nil || !n.Configured || n.ConfigFile != file {
				t.Fatalf("install = %+v, %v", n, err)
			}
			got := read(t, file)
			if !strings.Contains(got, "local dir = '"+w.probe.PluginDirs[0]+"'") || !strings.Contains(got, "require('crew').setup({})") {
				t.Errorf("plugin/crew.lua = %s", got)
			}
			for name, body := range c.files {
				if read(t, filepath.Join(w.probe.NvimConfigDir, name)) != body {
					t.Errorf("%s was edited", name)
				}
			}
			if _, err := w.probe.InstallNvim(ctx); err != nil || read(t, file) != got {
				t.Errorf("second install changed it: %v", err)
			}
			removed, err := w.probe.RemoveNvim(ctx)
			if err != nil || !removed {
				t.Fatalf("remove = %v, %v", removed, err)
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Error("remove left the file")
			}
			if removed, _ := w.probe.RemoveNvim(ctx); removed {
				t.Error("second remove reported a change")
			}
		})
	}
}

func TestNvimSetupLeavesAUsersOwnFileAlone(t *testing.T) {
	w := newWorld(t, true)
	if err := os.MkdirAll(w.probe.PluginDirs[0], 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(w.probe.NvimConfigDir, "plugin", "crew.lua")
	write(t, file, "print('mine')\n")
	ctx := context.Background()
	if _, err := w.probe.InstallNvim(ctx); err == nil {
		t.Error("install overwrote a file crew did not write")
	}
	if removed, err := w.probe.RemoveNvim(ctx); removed || err == nil {
		t.Errorf("remove = %v, %v on the user's own file", removed, err)
	}
	if read(t, file) != "print('mine')\n" {
		t.Error("the user's file changed")
	}
}

func TestInstallNvimRefusesWithoutThePluginFiles(t *testing.T) {
	w := newWorld(t, true)
	if _, err := w.probe.InstallNvim(context.Background()); err == nil {
		t.Error("installed a file pointing at a missing plugin dir")
	}
	if _, err := os.Stat(w.probe.NvimConfigDir); !os.IsNotExist(err) {
		t.Error("config dir written anyway")
	}
}
