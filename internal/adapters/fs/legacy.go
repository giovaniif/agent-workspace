package fs

import (
	"os"
	"path/filepath"

	"github.com/giovaniif/crew/internal/domain"
)

func AdoptRepoConfig(repo string) {
	old := filepath.Join(repo, domain.LegacyRepoConfigFile)
	current := filepath.Join(repo, domain.RepoConfigFile)
	info, err := os.Lstat(old)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if _, err := os.Lstat(current); err == nil {
		return
	}
	data, err := os.ReadFile(old)
	if err != nil {
		return
	}
	if err := os.WriteFile(old+".bak", data, info.Mode().Perm()); err != nil {
		return
	}
	if err := os.WriteFile(current, data, info.Mode().Perm()); err != nil {
		return
	}
	_ = os.Chmod(current, info.Mode().Perm())
	_ = os.Remove(old)
}
