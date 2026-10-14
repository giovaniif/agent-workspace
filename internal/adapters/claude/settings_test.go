package claude_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
)

const bin = "/opt/agentws/bin/agentws"

func settingsIn(t *testing.T, fixture string) (path string, original []byte) {
	t.Helper()
	original, err := os.ReadFile(filepath.Join("testdata", "settings", fixture))
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, original
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSetupMatchesGoldenAndIsIdempotent(t *testing.T) {
	path, _ := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	first := read(t, path)
	golden := read(t, filepath.Join("testdata", "settings", "user.setup.golden.json"))
	if !bytes.Equal(first, golden) {
		t.Fatalf("setup output differs from golden:\n%s", first)
	}
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	if second := read(t, path); !bytes.Equal(first, second) {
		t.Fatalf("second setup changed the file:\n%s", second)
	}
}

func TestSetupBacksUpTheOriginal(t *testing.T) {
	path, original := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	if got := read(t, claude.BackupPath(path)); !bytes.Equal(got, original) {
		t.Fatalf("backup = %s", got)
	}
}

func TestRemoveRestoresTheOriginalBytes(t *testing.T) {
	path, original := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	if err := claude.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !bytes.Equal(got, original) {
		t.Fatalf("remove did not restore the original:\n%s", got)
	}
	if _, err := os.Stat(claude.BackupPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup left behind: %v", err)
	}
}

func TestRemoveKeepsEditsMadeAfterSetup(t *testing.T) {
	path, _ := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(read(t, path)), `"theme": "dark"`, `"theme": "light"`, 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claude.Remove(path); err != nil {
		t.Fatal(err)
	}
	got := string(read(t, path))
	if !strings.Contains(got, `"theme": "light"`) {
		t.Fatalf("edit lost:\n%s", got)
	}
	if strings.Contains(got, "agentws") {
		t.Fatalf("agentws entries left:\n%s", got)
	}
	if !strings.Contains(got, `"command": "~/.claude/statusline.sh"`) {
		t.Fatalf("user status line not restored:\n%s", got)
	}
}

func TestSetupKeepsTheUsersHooks(t *testing.T) {
	path, original := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	before := hookCommands(t, original)
	after := hookCommands(t, read(t, path))
	for event, cmds := range before {
		if len(after[event]) < len(cmds) {
			t.Fatalf("%s lost hooks: %v -> %v", event, cmds, after[event])
		}
		for i, c := range cmds {
			if after[event][i] != c {
				t.Fatalf("%s hook %d = %q, want the user's %q first", event, i, after[event][i], c)
			}
		}
	}
	for _, event := range claude.HookEvents {
		cmds := after[event]
		want := bin + " hook --harness claude --event " + event
		if len(cmds) == 0 || cmds[len(cmds)-1] != want {
			t.Errorf("%s hooks = %v, want %q last", event, cmds, want)
		}
	}
}

func TestSetupChainsTheUsersStatusLine(t *testing.T) {
	path, _ := settingsIn(t, "user.json")
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	var s struct {
		StatusLine map[string]any `json:"statusLine"`
	}
	if err := json.Unmarshal(read(t, path), &s); err != nil {
		t.Fatal(err)
	}
	cmd, _ := s.StatusLine["command"].(string)
	if chain, ok := claude.ChainedStatusLine(cmd); !ok || chain != "~/.claude/statusline.sh" {
		t.Fatalf("status line command %q chains %q, %v", cmd, chain, ok)
	}
	if s.StatusLine["padding"] != float64(1) {
		t.Fatalf("status line options lost: %v", s.StatusLine)
	}
}

func TestSetupWithANewBinaryReplacesItsOwnEntries(t *testing.T) {
	path, _ := settingsIn(t, "user.json")
	if err := claude.Setup(path, "/old/agentws"); err != nil {
		t.Fatal(err)
	}
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	golden := read(t, filepath.Join("testdata", "settings", "user.setup.golden.json"))
	if got := read(t, path); !bytes.Equal(got, golden) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetupAndRemoveWithoutASettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude", "settings.json")
	for range 2 {
		if err := claude.Setup(path, bin); err != nil {
			t.Fatal(err)
		}
	}
	cmds := hookCommands(t, read(t, path))
	if len(cmds["Stop"]) != 1 {
		t.Fatalf("hooks = %v", cmds)
	}
	if err := claude.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings file left behind: %v", err)
	}
}

func TestRemoveWithoutSetupChangesNothing(t *testing.T) {
	path, original := settingsIn(t, "user.json")
	if err := claude.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !bytes.Equal(got, original) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetupRefusesInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claude.Setup(path, bin); err == nil {
		t.Fatal("setup accepted invalid JSON")
	}
	if got := read(t, path); string(got) != "{nope" {
		t.Fatalf("file changed: %s", got)
	}
}

func hookCommands(t *testing.T, settings []byte) map[string][]string {
	t.Helper()
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for event, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[event] = append(out[event], h.Command)
			}
		}
	}
	return out
}
