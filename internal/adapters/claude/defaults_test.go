package claude_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
)

func TestReadDefaultsTakesModelAndEffortFromSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus","effortLevel":"low","hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := claude.ReadDefaults(path)
	if err != nil || model != "opus" || effort != "low" {
		t.Fatalf("got %q, %q, %v", model, effort, err)
	}
	model, effort, err = claude.ReadDefaults(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || model != "" || effort != "" {
		t.Fatalf("missing file: %q, %q, %v", model, effort, err)
	}
}

func TestReadEffortByModelKeysEachModelsEffortByItsShortName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := `{"modelSettings":{"claude-opus-5-5":{"effortLevel":"low"},"claude-sonnet-5-5[1m]":{"effortLevel":"high"},"claude-haiku-4-5":{}}}`
	if err := os.WriteFile(path, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := claude.ReadEffortByModel(path)
	if err != nil || len(got) != 2 || got["opus-5.5"] != "low" || got["sonnet-5.5"] != "high" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestReadDefaultsShortensAFullModelIDAsTheEffortKeysAre(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"claude-opus-5-5","modelSettings":{"claude-opus-5-5":{"effortLevel":"low"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	model, _, err := claude.ReadDefaults(path)
	byModel, _ := claude.ReadEffortByModel(path)
	if err != nil || model != "opus-5.5" || byModel[model] != "low" {
		t.Fatalf("model %q, efforts %v, %v; want opus-5.5 to find low", model, byModel, err)
	}
}
