//go:build integration

package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/app"
)

var _ app.RepoInspector = git.Inspector{}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRepoWithOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin.git")
	run(t, tmp, "init", "-q", "--bare", "-b", "trunk", origin)
	seed := filepath.Join(tmp, "seed")
	run(t, tmp, "clone", "-q", origin, seed)
	write(t, filepath.Join(seed, "a.txt"), "a")
	write(t, filepath.Join(seed, "b.txt"), "b")
	run(t, seed, "add", ".")
	run(t, seed, "commit", "-q", "-m", "init")
	run(t, seed, "push", "-q", "origin", "HEAD:trunk")

	repo := filepath.Join(tmp, "repo")
	run(t, tmp, "clone", "-q", origin, repo)
	run(t, repo, "checkout", "-q", "-b", "feat/x")
	write(t, filepath.Join(repo, "a.txt"), "changed")
	write(t, filepath.Join(repo, "untracked.txt"), "u")
	run(t, repo, "mv", "b.txt", "c.txt")

	facts, err := git.Inspector{}.Inspect(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := app.RepoFacts{DefaultBranch: "trunk", Branch: "feat/x", ChangedFiles: 3}
	if facts != want {
		t.Errorf("facts = %+v, want %+v", facts, want)
	}
}

func TestInspectRepoWithoutOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run(t, repo, "init", "-q", "-b", "main")
	facts, err := git.Inspector{}.Inspect(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := app.RepoFacts{Branch: "main"}
	if facts != want {
		t.Errorf("facts = %+v, want %+v", facts, want)
	}
}

func TestInspectNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := (git.Inspector{}).Inspect(context.Background(), t.TempDir()); err == nil {
		t.Fatal("want an error outside a repo")
	}
}
