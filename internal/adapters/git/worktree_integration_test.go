//go:build integration

package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.WorktreeLister = git.Worktrees{}

func TestWorktreeDetectListFromAnyCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "api")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-q", "-b", "main")
	write(t, filepath.Join(repo, "a.txt"), "a")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "init")
	feat := filepath.Join(tmp, "api-feat")
	run(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	gone := filepath.Join(tmp, "api-gone")
	run(t, repo, "worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	want := domain.RepoListing{Main: repo, Worktrees: []domain.ListedWorktree{{Path: feat, Branch: "feat"}}}
	for _, dir := range []string{repo, feat} {
		got, err := git.Worktrees{}.ListWorktrees(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("from %s: got %+v, want %+v", dir, got, want)
		}
	}
	if _, err := (git.Worktrees{}).ListWorktrees(context.Background(), tmp); err == nil {
		t.Error("want an error outside a repo")
	}
}
