package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomationListAdoptsAnOldRepoConfig(t *testing.T) {
	repo := t.TempDir()
	body := "[[automation]]\nname = \"deps\"\nagent = \"codex\"\nprompt = \"Repo deps\"\n"
	if err := os.WriteFile(filepath.Join(repo, ".agentws.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := configDaemon(t, globalAutomations)
	got := listAutomations(t, c, repo)
	if len(got.Automations) == 0 || got.Automations[0].Name != "deps" || got.Automations[0].Source != filepath.Join(repo, ".crew.toml") {
		t.Fatalf("automations = %+v, want deps from the adopted .crew.toml", got.Automations)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agentws.toml.bak")); err != nil {
		t.Fatalf("no backup of the old config: %v", err)
	}
}
