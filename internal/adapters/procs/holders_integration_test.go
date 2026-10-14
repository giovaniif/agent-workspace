//go:build integration

package procs_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/procs"
	"github.com/giovaniif/crew/internal/app"
)

var _ app.WorktreeHolders = procs.Table{}

func TestCleanupHoldersFindsASleepingProcessInsideTheWorktree(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not installed")
	}
	tmp := t.TempDir()
	held, free := filepath.Join(tmp, "held"), filepath.Join(tmp, "free")
	for _, d := range []string{filepath.Join(held, "src"), free} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sleeper := exec.Command("sleep", "30")
	sleeper.Dir = filepath.Join(held, "src")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })

	got, err := procs.Table{}.Holders(context.Background(), []string{held, free})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[held]) != 1 || !strings.HasPrefix(got[held][0], "sleep (pid ") {
		t.Errorf("held = %v, want the sleeper", got[held])
	}
	if len(got[free]) != 0 {
		t.Errorf("free = %v, want nobody", got[free])
	}
}
