package omp

import (
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

func retireLegacyHook(dir string) bool {
	if !HasLegacyHook(dir) {
		return false
	}
	file := LegacyHookFile(dir)
	return os.Rename(file, file+".bak") == nil
}
