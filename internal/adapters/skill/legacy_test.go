package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/skill"
)

func writeLegacy(t *testing.T, dir, body string) string {
	t.Helper()
	file := filepath.Join(dir, "skills", "agentws-browser", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestInstallMovesTheOldSkillAsideWithABackup(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, "<!-- agentws:browser-skill -->\nold text\n")
	if _, err := skill.Install(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old skill still loads: %v", err)
	}
	if b, err := os.ReadFile(old + ".bak"); err != nil || string(b) != "<!-- agentws:browser-skill -->\nold text\n" {
		t.Fatalf("backup = %q, %v", b, err)
	}
	if !skill.Installed(dir) {
		t.Fatal("the crew skill is not installed")
	}
}

func TestInstallLeavesAnOldSkillFileOfTheUsersAlone(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, "my notes\n")
	if _, err := skill.Install(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(old); string(b) != "my notes\n" {
		t.Fatalf("changed: %q", b)
	}
}

func TestRemoveMovesTheOldSkillAside(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, "<!-- agentws:browser-skill -->\nold text\n")
	changed, err := skill.Remove(dir)
	if err != nil || !changed {
		t.Fatalf("remove = %v, %v", changed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old skill still loads: %v", err)
	}
}

func TestInstallKeepsAnEarlierBackupOfTheOldSkill(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, "<!-- agentws:browser-skill -->\nold text\n")
	if err := os.WriteFile(old+".bak", []byte("earlier backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Install(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(old + ".bak"); string(b) != "earlier backup" {
		t.Fatalf("earlier backup = %q", b)
	}
	if b, err := os.ReadFile(old + ".bak.1"); err != nil || string(b) != "<!-- agentws:browser-skill -->\nold text\n" {
		t.Fatalf("new backup = %q, %v", b, err)
	}
}
