//go:build integration

package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/app"
)

var _ app.WorktreeAdder = git.Adder{}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", rev)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

func TestAddWorktreeBranchesFromTheBaseIntoANewDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	run(t, root, "init", "-q", "-b", "main", origin)
	write(t, filepath.Join(origin, "a.txt"), "a")
	run(t, origin, "add", ".")
	run(t, origin, "commit", "-qm", "one")
	clone := filepath.Join(root, "api")
	run(t, root, "clone", "-q", origin, clone)
	write(t, filepath.Join(clone, "b.txt"), "local")
	run(t, clone, "add", ".")
	run(t, clone, "commit", "-qm", "local only")

	path := filepath.Join(root, "worktrees", "api", "eng-1")
	added, err := (git.Adder{}).AddWorktree(context.Background(), clone, path, "eng-1", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	realClone, _ := filepath.EvalSymlinks(clone)
	realPath, _ := filepath.EvalSymlinks(path)
	if added != (app.AddedWorktree{Main: realClone, Path: realPath}) {
		t.Fatalf("added %+v, want main %s and path %s as git reports them", added, realClone, realPath)
	}
	if got, want := revParse(t, path, "HEAD"), revParse(t, clone, "origin/main"); got != want {
		t.Fatalf("worktree HEAD %s, want origin/main %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(path, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("worktree has the clone's local commit: %v", err)
	}
	cmd := exec.Command("git", "-C", path, "symbolic-ref", "--short", "HEAD")
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "eng-1" {
		t.Fatalf("branch %q, %v", out, err)
	}

	_, err = (git.Adder{}).AddWorktree(context.Background(), clone, filepath.Join(root, "other"), "eng-1", "origin/main")
	if err == nil || !strings.Contains(err.Error(), "eng-1") {
		t.Fatalf("adding a taken branch again: %v", err)
	}
}
