package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBinary = "/opt/crew/bin/crew"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "setup", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func codexHome(t *testing.T, hooksJSON []byte) (SetupConfig, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	if hooksJSON != nil {
		if err := os.WriteFile(path, hooksJSON, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tick := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	return SetupConfig{
		Dir:     dir,
		Command: testBinary,
		Now: func() time.Time {
			tick = tick.Add(time.Second)
			return tick
		},
	}, path
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "hooks.json.crew-*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSetupMergesIntoExistingHooksAndBacksUpTheOriginal(t *testing.T) {
	original := readFixture(t, "existing.json")
	cfg, path := codexHome(t, original)
	res, err := Setup(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("setup reported no change")
	}
	if got, want := mustRead(t, path), readFixture(t, "existing.setup.golden.json"); !bytes.Equal(got, want) {
		t.Fatalf("hooks.json differs from golden\n got: %s\nwant: %s", got, want)
	}
	saved := backups(t, cfg.Dir)
	if len(saved) != 1 || res.Backup != saved[0] {
		t.Fatalf("backups %v, result %q", saved, res.Backup)
	}
	if !bytes.Equal(mustRead(t, saved[0]), original) {
		t.Fatal("backup is not the original file")
	}
}

func TestSetupTwiceIsByteIdenticalAndTakesNoSecondBackup(t *testing.T) {
	cfg, path := codexHome(t, readFixture(t, "existing.json"))
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	first := mustRead(t, path)
	res, err := Setup(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Backup != "" {
		t.Fatalf("second setup: %+v", res)
	}
	if !bytes.Equal(mustRead(t, path), first) {
		t.Fatal("second setup changed the file")
	}
	if n := len(backups(t, cfg.Dir)); n != 1 {
		t.Fatalf("%d backups", n)
	}
}

func TestRemoveRestoresTheOriginalFile(t *testing.T) {
	original := readFixture(t, "existing.json")
	cfg, path := codexHome(t, original)
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	res, err := Remove(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("remove reported no change")
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("remove did not restore the original\n got: %s\nwant: %s", got, original)
	}
}

func TestSetupCreatesMissingFileWithoutBackupAndRemoveDeletesIt(t *testing.T) {
	cfg, path := codexHome(t, nil)
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	if got, want := mustRead(t, path), readFixture(t, "fresh.setup.golden.json"); !bytes.Equal(got, want) {
		t.Fatalf("hooks.json differs from golden\n got: %s\nwant: %s", got, want)
	}
	if n := len(backups(t, cfg.Dir)); n != 0 {
		t.Fatalf("%d backups of a file that did not exist", n)
	}
	if _, err := Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("hooks.json left behind: %v", err)
	}
}

func TestRemoveWithNothingToRemoveTouchesNothing(t *testing.T) {
	original := readFixture(t, "existing.json")
	cfg, path := codexHome(t, original)
	res, err := Remove(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Backup != "" || !bytes.Equal(mustRead(t, path), original) || len(backups(t, cfg.Dir)) != 0 {
		t.Fatalf("remove touched a file without our hooks: %+v", res)
	}

	empty, _ := codexHome(t, nil)
	if res, err := Remove(empty); err != nil || res.Changed {
		t.Fatalf("remove without hooks.json: %+v, %v", res, err)
	}
}

func TestSetupMovesOurHooksWhenTheBinaryMoves(t *testing.T) {
	cfg, path := codexHome(t, readFixture(t, "existing.json"))
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Command = "/usr/local/bin/crew"
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, path))
	if strings.Contains(got, testBinary) || strings.Count(got, "/usr/local/bin/crew hook --harness codex --event Stop") != 1 {
		t.Fatalf("hooks.json:\n%s", got)
	}
}

func TestSetupKeepsHooksTheUserAddedAfterOurs(t *testing.T) {
	cfg, path := codexHome(t, readFixture(t, "existing.json"))
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	withUserGroup := strings.Replace(string(mustRead(t, path)),
		`"Stop": [`,
		`"Stop": [
      {"hooks": [{"type": "command", "command": "/home/dev/hooks/after.sh"}]},`, 1)
	if err := os.WriteFile(path, []byte(withUserGroup), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Setup(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("setup rewrote a file that already has our hooks")
	}
	if !strings.Contains(string(mustRead(t, path)), "/home/dev/hooks/after.sh") {
		t.Fatal("user hook lost")
	}
}

func TestSetupRefusesMalformedHooksFileAndLeavesItAlone(t *testing.T) {
	broken := []byte(`{"hooks": {`)
	cfg, path := codexHome(t, broken)
	if _, err := Setup(cfg); err == nil {
		t.Fatal("malformed hooks.json accepted")
	}
	if !bytes.Equal(mustRead(t, path), broken) || len(backups(t, cfg.Dir)) != 0 {
		t.Fatal("malformed hooks.json was modified")
	}
}

func TestSetupQuotesABinaryPathWithSpaces(t *testing.T) {
	cfg, path := codexHome(t, nil)
	cfg.Command = "/Users/dev/My Tools/crew"
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mustRead(t, path)), `'/Users/dev/My Tools/crew' hook --harness codex --event Stop`) {
		t.Fatalf("hooks.json:\n%s", mustRead(t, path))
	}
	if _, err := Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("quoted hooks were not recognised on remove")
	}
}
