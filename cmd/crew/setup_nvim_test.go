package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupNvimWritesAndRemovesItsPluginFile(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{
		"HOME":            root,
		"XDG_CONFIG_HOME": filepath.Join(root, "cfg"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
	}
	if err := os.MkdirAll(filepath.Join(root, "data", "agentws", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	init := filepath.Join(root, "cfg", "nvim", "plugin", "agentws.lua")
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := runSetup(append([]string{"nvim"}, args...), &out, &errOut, func(k string) string { return env[k] }, "/opt/agentws/bin/agentws")
		return code, out.String() + errOut.String()
	}

	code, out := run()
	b, _ := os.ReadFile(init)
	if code != 0 || !strings.Contains(out, init) || !strings.Contains(string(b), "require('agentws').setup({})") {
		t.Fatalf("setup nvim: code %d, %q\n%s", code, out, b)
	}
	code, out = run("--remove")
	if _, err := os.Stat(init); code != 0 || !os.IsNotExist(err) {
		t.Fatalf("remove: code %d, %q", code, out)
	}
}
