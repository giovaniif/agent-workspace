package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
)

func writeLegacyFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

func readLegacyFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAnOldRepoConfigMovesToTheNewNameAndKeepsABackup(t *testing.T) {
	repo := t.TempDir()
	writeLegacyFile(t, filepath.Join(repo, ".agentws.toml"), "[setup]\nrun = [\"make\"]\n")
	wsfs.AdoptRepoConfig(repo)
	if got := readLegacyFile(t, filepath.Join(repo, ".crew.toml")); got != "[setup]\nrun = [\"make\"]\n" {
		t.Fatalf(".crew.toml = %q", got)
	}
	if got := readLegacyFile(t, filepath.Join(repo, ".agentws.toml.bak")); got != "[setup]\nrun = [\"make\"]\n" {
		t.Fatalf("backup = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agentws.toml")); err == nil {
		t.Fatal("the old file stays next to the new one")
	}
	info, err := os.Stat(filepath.Join(repo, ".crew.toml"))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, %v", info, err)
	}
}

func TestARepoThatHasANewConfigIsLeftAlone(t *testing.T) {
	repo := t.TempDir()
	writeLegacyFile(t, filepath.Join(repo, ".agentws.toml"), "old")
	writeLegacyFile(t, filepath.Join(repo, ".crew.toml"), "new")
	wsfs.AdoptRepoConfig(repo)
	if readLegacyFile(t, filepath.Join(repo, ".crew.toml")) != "new" || readLegacyFile(t, filepath.Join(repo, ".agentws.toml")) != "old" {
		t.Fatal("a file changed")
	}
}

func TestARepoWithoutAnyConfigStaysEmpty(t *testing.T) {
	repo := t.TempDir()
	wsfs.AdoptRepoConfig(repo)
	entries, _ := os.ReadDir(repo)
	if len(entries) != 0 {
		t.Fatalf("created %v", entries)
	}
}
