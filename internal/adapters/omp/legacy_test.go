package omp

import (
	"os"
	"strings"
	"testing"
)

const legacyHook = "// agentws: written by agentws setup omp; agentws setup omp --remove deletes this file.\nconst agentws = \"/old/agentws\";\n"

func TestSetupMovesTheOldHookAsideWithABackup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, LegacyHookFile(dir), legacyHook)
	res, err := Setup(SetupConfig{Dir: dir, Command: testBin})
	if err != nil || !res.Changed {
		t.Fatalf("setup = %+v, %v", res, err)
	}
	if _, err := os.Stat(LegacyHookFile(dir)); !os.IsNotExist(err) {
		t.Fatalf("old hook still runs: %v", err)
	}
	if got := read(t, LegacyHookFile(dir)+".bak"); got != legacyHook {
		t.Fatalf("backup:\n%s", got)
	}
}

func TestSetupLeavesAnOldHookFileOfTheUsersAlone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, LegacyHookFile(dir), "export default () => {}\n")
	if _, err := Setup(SetupConfig{Dir: dir, Command: testBin}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, LegacyHookFile(dir)); !strings.Contains(got, "export default") {
		t.Fatalf("changed: %q", got)
	}
}

func TestRemoveMovesTheOldHookAside(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, LegacyHookFile(dir), legacyHook)
	res, err := Remove(SetupConfig{Dir: dir})
	if err != nil || !res.Changed {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if _, err := os.Stat(LegacyHookFile(dir)); !os.IsNotExist(err) {
		t.Fatalf("old hook still runs: %v", err)
	}
}

func TestSetupKeepsAnEarlierBackupOfTheOldHook(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, LegacyHookFile(dir), legacyHook)
	writeFile(t, LegacyHookFile(dir)+".bak", "earlier backup")
	if _, err := Setup(SetupConfig{Dir: dir, Command: testBin}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, LegacyHookFile(dir)+".bak"); got != "earlier backup" {
		t.Fatalf("earlier backup = %q", got)
	}
	if got := read(t, LegacyHookFile(dir)+".bak.1"); got != legacyHook {
		t.Fatalf("new backup = %q", got)
	}
}
