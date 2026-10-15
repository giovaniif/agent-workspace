package claude_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
)

func TestInstalledIsTrueOnlyAfterSetupWithTheSameBinary(t *testing.T) {
	path, original := settingsIn(t, "user.json")
	if ok, err := claude.Installed(path, bin); err != nil || ok {
		t.Fatalf("before setup: installed = %v, %v", ok, err)
	}
	if string(read(t, path)) != string(original) {
		t.Fatal("Installed changed the settings file")
	}
	if err := claude.Setup(path, bin); err != nil {
		t.Fatal(err)
	}
	if ok, err := claude.Installed(path, bin); err != nil || !ok {
		t.Errorf("after setup: installed = %v, %v", ok, err)
	}
	if ok, _ := claude.Installed(path, "/elsewhere/crew"); ok {
		t.Error("hooks pointing at another binary count as installed")
	}
}

func TestInstalledWithNoSettingsFileIsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if ok, err := claude.Installed(path, bin); err != nil || ok {
		t.Errorf("installed = %v, %v", ok, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Installed created the settings file")
	}
}

func TestInstalledReportsAnUnreadableSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := claude.Installed(path, bin); err == nil {
		t.Error("broken JSON gives no error")
	}
}
