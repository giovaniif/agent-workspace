package omp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const legacyMarker = "// agentws: written by agentws setup omp"

func LegacyHookFile(dir string) string { return filepath.Join(dir, "hooks", "post", "agentws.ts") }

func HasLegacyHook(dir string) bool {
	b, err := os.ReadFile(LegacyHookFile(dir))
	return err == nil && strings.HasPrefix(string(b), legacyMarker)
}

func retireLegacyHook(dir string) (bool, error) {
	if !HasLegacyHook(dir) {
		return false, nil
	}
	file := LegacyHookFile(dir)
	if err := os.Rename(file, freeBackup(file)); err != nil {
		return false, fmt.Errorf("move the old hook aside: %w", err)
	}
	return true, nil
}

func freeBackup(file string) string {
	backup := file + ".bak"
	for n := 1; ; n++ {
		if _, err := os.Lstat(backup); err != nil {
			return backup
		}
		backup = fmt.Sprintf("%s.bak.%d", file, n)
	}
}
