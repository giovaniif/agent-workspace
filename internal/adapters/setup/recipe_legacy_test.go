package setup_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/setup"
)

func TestRecipeLoadAdoptsAnOldRepoConfig(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".agentws.toml"), "[setup]\nrun = [\"make\"]\n")
	got, found, err := setup.Recipes{}.Load(repo)
	if err != nil || !found || !reflect.DeepEqual(got.Run, []string{"make"}) {
		t.Fatalf("Load = %+v, %v, %v", got, found, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".crew.toml")); err != nil {
		t.Fatalf("the config did not move to the new name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agentws.toml.bak")); err != nil {
		t.Fatalf("no backup of the old config: %v", err)
	}
}
