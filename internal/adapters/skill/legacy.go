package skill

import (
	"os"
	"path/filepath"
	"strings"
)

const legacyMarker = "<!-- agentws:browser-skill -->"

func legacyPath(dir string) string {
	return filepath.Join(dir, "skills", "agentws-browser", "SKILL.md")
}

func retireLegacy(dir string) bool {
	file := legacyPath(dir)
	got, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(got), legacyMarker) {
		return false
	}
	return os.Rename(file, file+".bak") == nil
}
