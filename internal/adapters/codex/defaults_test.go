package codex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/codex"
)

func TestReadDefaultsTakesModelAndEffortFromConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt-6-sol\"\nmodel_reasoning_effort = \"medium\"\n[projects]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := codex.ReadDefaults(path)
	if err != nil || model != "gpt-6-sol" || effort != "medium" {
		t.Fatalf("got %q, %q, %v", model, effort, err)
	}
	model, effort, err = codex.ReadDefaults(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || model != "" || effort != "" {
		t.Fatalf("missing file: %q, %q, %v", model, effort, err)
	}
}
