package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "crew-cmd-home-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME":              home,
		"CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude"),
		"CODEX_HOME":        filepath.Join(home, ".codex"),
		"XDG_CONFIG_HOME":   filepath.Join(home, ".config"),
		"XDG_DATA_HOME":     filepath.Join(home, ".local", "share"),
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
