package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupCodex(t *testing.T, codexHome string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := func(k string) string {
		if k == "CODEX_HOME" {
			return codexHome
		}
		return ""
	}
	code := runSetup(append([]string{"codex"}, args...), &out, &errOut, env, "/opt/agentws/bin/agentws")
	return code, out.String(), errOut.String()
}

func TestSetupCodexWritesHooksAndPrintsTheTrustStep(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := setupCodex(t, dir)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	hooks, err := os.ReadFile(filepath.Join(dir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hooks), "/opt/agentws/bin/agentws hook --harness codex --event Stop") {
		t.Fatalf("hooks.json:\n%s", hooks)
	}
	if !strings.Contains(out, filepath.Join(dir, "hooks.json")) || !strings.Contains(out, "trust") {
		t.Fatalf("stdout %q", out)
	}
}

func TestSetupCodexTwiceSaysNothingChanged(t *testing.T) {
	dir := t.TempDir()
	setupCodex(t, dir)
	code, out, _ := setupCodex(t, dir)
	if code != 0 || !strings.Contains(out, "already set up") || strings.Contains(out, "trust") {
		t.Fatalf("code %d, stdout %q", code, out)
	}
}

func TestSetupCodexRemoveUndoesIt(t *testing.T) {
	dir := t.TempDir()
	setupCodex(t, dir)
	code, out, _ := setupCodex(t, dir, "--remove")
	if code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json left behind: %v", err)
	}
	if _, out, _ := setupCodex(t, dir, "--remove"); !strings.Contains(out, "nothing to remove") {
		t.Fatalf("stdout %q", out)
	}
}

func TestSetupCodexFailsOnBrokenHooksFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := setupCodex(t, dir)
	if code != 1 || !strings.Contains(errOut, "hooks.json") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestSetupRejectsAnUnknownHarness(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runSetup([]string{"vim"}, &out, &errOut, func(string) string { return "" }, "agentws"); code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: crew setup") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestSetupCodexAlsoInstallsTheBrowserSkillAndRemoveTakesItAway(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "skills", "agentws-browser", "SKILL.md")
	if code, _, errOut := setupCodex(t, dir); code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if body, err := os.ReadFile(file); err != nil || !strings.Contains(string(body), "agentws browser snapshot") {
		t.Fatalf("skill: %v", err)
	}
	if code, _, _ := setupCodex(t, dir, "--remove"); code != 0 {
		t.Fatal("remove")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("skill left: %v", err)
	}
}
