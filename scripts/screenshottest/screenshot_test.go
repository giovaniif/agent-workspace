package screenshottest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeAgentws = `#!/bin/sh
onboarded=no
[ -e "$AGENTWS_HOME/onboarded" ] && onboarded=yes
echo "agentws $* HOME=$HOME XDG_CONFIG_HOME=$XDG_CONFIG_HOME XDG_DATA_HOME=$XDG_DATA_HOME CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR CODEX_HOME=$CODEX_HOME onboarded=$onboarded" >> "$SHOT_LOG"
`

const fakeTmux = `#!/bin/sh
echo "tmux HOME=$HOME XDG_CONFIG_HOME=$XDG_CONFIG_HOME $*" >> "$SHOT_LOG"
`

const fakeFreeze = `#!/bin/sh
cat > /dev/null
`

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func runScreenshot(t *testing.T, steps func(realHome string) string) (string, string, error) {
	t.Helper()
	script, err := os.ReadFile("../screenshot")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "scripts/screenshot"), string(script), 0o755)
	write(t, filepath.Join(root, "Makefile"), "build:\n\t@true\n", 0o644)
	write(t, filepath.Join(root, "bin/agentws"), fakeAgentws, 0o755)
	fakes := t.TempDir()
	write(t, filepath.Join(fakes, "tmux"), fakeTmux, 0o755)
	write(t, filepath.Join(fakes, "freeze"), fakeFreeze, 0o755)
	realHome := t.TempDir()
	log := filepath.Join(t.TempDir(), "log")
	cmd := exec.Command(filepath.Join(root, "scripts/screenshot"), filepath.Join(t.TempDir(), "out.png"))
	cmd.Dir = t.TempDir()
	cmd.Stdin = strings.NewReader(steps(realHome))
	cmd.Env = append(os.Environ(),
		"HOME="+realHome,
		"XDG_CONFIG_HOME=",
		"XDG_DATA_HOME=",
		"CLAUDE_CONFIG_DIR=",
		"CODEX_HOME=",
		"PATH="+fakes+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SHOT_LOG="+log,
	)
	out, runErr := cmd.CombinedOutput()
	got, _ := os.ReadFile(log)
	t.Logf("output:\n%s\nlog:\n%s", out, got)
	return string(got), realHome, runErr
}

func TestScreenshotDaemonAndPaneNeverUseTheRealHome(t *testing.T) {
	log, realHome, err := runScreenshot(t, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log, realHome) {
		t.Fatalf("the real HOME %s reached a started process", realHome)
	}
	daemon := ""
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		for _, key := range []string{"HOME=/", "XDG_CONFIG_HOME=/"} {
			if !strings.Contains(line, key) {
				t.Fatalf("%q is unset in %q", key, line)
			}
		}
		if strings.HasPrefix(line, "agentws daemon start") {
			daemon = line
		}
	}
	for _, key := range []string{"XDG_DATA_HOME=/", "CLAUDE_CONFIG_DIR=/", "CODEX_HOME=/", "onboarded=yes"} {
		if !strings.Contains(daemon, key) {
			t.Fatalf("daemon start lacks %q: %q", key, daemon)
		}
	}
}

func TestScreenshotRefusesARunStepWithTheRealHome(t *testing.T) {
	log, realHome, err := runScreenshot(t, func(realHome string) string {
		return "run env HOME=" + realHome + " agentws tui\n"
	})
	if err == nil {
		t.Fatalf("a run step with HOME=%s was accepted", realHome)
	}
	if strings.Contains(log, "send-keys") {
		t.Fatalf("the pane ran a command before refusing: %s", log)
	}
}
