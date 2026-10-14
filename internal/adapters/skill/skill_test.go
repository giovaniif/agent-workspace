package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/skill"
)

func TestInstallWritesTheBrowserSkillOnceAndRemoveTakesItAway(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "skills", "crew-browser", "SKILL.md")
	changed, err := skill.Install(dir)
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: crew-browser", "crew browser snapshot", "crew browser click @e1", "crew browser screenshot", "no browser: the macOS app is not connected"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("SKILL.md has no %q", want)
		}
	}
	if file != skill.Path(dir) {
		t.Fatalf("path %q", skill.Path(dir))
	}
	if changed, err := skill.Install(dir); err != nil || changed {
		t.Fatalf("second install: %v %v", changed, err)
	}
	if !skill.Installed(dir) {
		t.Fatal("not installed")
	}
	if changed, err := skill.Remove(dir); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("skill dir left: %v", err)
	}
	if changed, err := skill.Remove(dir); err != nil || changed {
		t.Fatalf("second remove: %v %v", changed, err)
	}
	if skill.Installed(dir) {
		t.Fatal("still installed")
	}
}

func TestASkillFileThatCrewDidNotWriteIsNeverTouched(t *testing.T) {
	dir := t.TempDir()
	file := skill.Path(dir)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("my own notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Install(dir); err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("install over a foreign file: %v", err)
	}
	if _, err := skill.Remove(dir); err == nil {
		t.Fatal("remove of a foreign file")
	}
	if body, _ := os.ReadFile(file); string(body) != "my own notes\n" {
		t.Fatalf("changed: %q", body)
	}
}

func TestAnOlderCrewSkillIsReplaced(t *testing.T) {
	dir := t.TempDir()
	file := skill.Path(dir)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(skill.Marker+"\nold text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := skill.Install(dir); err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if body, _ := os.ReadFile(file); strings.Contains(string(body), "old text") {
		t.Fatal("old skill kept")
	}
}
