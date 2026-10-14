//go:build integration

package git_test

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.CleanupGit = git.Worktrees{}

func cleanupGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func clonedRepo(t *testing.T) (tmp, repo string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(tmp, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, seed, "init", "-q", "-b", "main")
	write(t, filepath.Join(seed, "a.txt"), "a\n")
	write(t, filepath.Join(seed, ".gitignore"), "node_modules/\n")
	run(t, seed, "add", ".")
	run(t, seed, "commit", "-q", "-m", "init")
	run(t, tmp, "clone", "-q", "--bare", seed, filepath.Join(tmp, "origin.git"))
	run(t, tmp, "clone", "-q", filepath.Join(tmp, "origin.git"), "api")
	return tmp, filepath.Join(tmp, "api")
}

func TestCleanupFactsMergeStateAndDirt(t *testing.T) {
	tmp, repo := clonedRepo(t)
	ctx := context.Background()
	g := git.Worktrees{}

	fresh := filepath.Join(tmp, "api-fresh")
	run(t, repo, "worktree", "add", "-q", "-b", "fresh", fresh)
	merged := filepath.Join(tmp, "api-merged")
	run(t, repo, "worktree", "add", "-q", "-b", "merged", merged)
	write(t, filepath.Join(merged, "b.txt"), "b\n")
	run(t, merged, "add", ".")
	run(t, merged, "commit", "-q", "-m", "b")
	run(t, repo, "merge", "-q", "--ff-only", "merged")
	run(t, repo, "push", "-q", "origin", "main")
	run(t, repo, "fetch", "-q")
	unmerged := filepath.Join(tmp, "api-unmerged")
	run(t, repo, "worktree", "add", "-q", "-b", "unmerged", unmerged)
	write(t, filepath.Join(unmerged, "c.txt"), "c\n")
	run(t, unmerged, "add", ".")
	run(t, unmerged, "commit", "-q", "-m", "c")

	for _, c := range []struct {
		path, branch string
		inDefault    bool
	}{{fresh, "fresh", true}, {merged, "merged", true}, {unmerged, "unmerged", false}} {
		f, err := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: c.path, Branch: c.branch})
		if err != nil {
			t.Fatal(err)
		}
		if f.InDefault != c.inDefault || f.OnDefault || f.Uncommitted != 0 || f.ModifiedAt.IsZero() || f.Fingerprint == "" {
			t.Errorf("%s: facts = %+v, want InDefault %v and clean", c.branch, f, c.inDefault)
		}
	}

	before, _ := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: fresh, Branch: "fresh"})
	write(t, filepath.Join(fresh, "a.txt"), "changed\n")
	write(t, filepath.Join(fresh, "new.txt"), "new\n")
	if err := os.MkdirAll(filepath.Join(fresh, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fresh, "node_modules", "dep.js"), "x")
	after, err := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: fresh, Branch: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if after.Uncommitted != 2 || after.Fingerprint == before.Fingerprint {
		t.Errorf("dirty facts = %+v (before %q), want 2 changes and a new fingerprint", after, before.Fingerprint)
	}
	write(t, filepath.Join(fresh, "a.txt"), "changed again\n")
	again, err := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: fresh, Branch: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Uncommitted != 2 || again.Fingerprint == after.Fingerprint {
		t.Errorf("second edit kept fingerprint %q; want a new one when tracked content changes", again.Fingerprint)
	}

	main, err := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: repo, Branch: "main"})
	if err != nil || !main.OnDefault {
		t.Errorf("main checkout facts = %+v, %v, want OnDefault", main, err)
	}
	if _, err := g.CleanupFacts(ctx, domain.Worktree{Repo: repo, Path: filepath.Join(tmp, "nope")}); err == nil {
		t.Error("want an error for a missing worktree")
	}
}

func TestCleanupFactsWithoutOriginAreNeverInDefault(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, tmp, "init", "-q", "-b", "main")
	write(t, filepath.Join(tmp, "a.txt"), "a")
	run(t, tmp, "add", ".")
	run(t, tmp, "commit", "-q", "-m", "init")
	wt := filepath.Join(tmp, "wt")
	run(t, tmp, "worktree", "add", "-q", "-b", "x", wt)
	f, err := git.Worktrees{}.CleanupFacts(context.Background(), domain.Worktree{Repo: tmp, Path: wt, Branch: "x"})
	if err != nil || f.InDefault {
		t.Errorf("facts = %+v, %v; want not InDefault with no origin", f, err)
	}
}

func TestCleanupBackupWritesPatchUntrackedAndStatus(t *testing.T) {
	tmp, repo := clonedRepo(t)
	wt := filepath.Join(tmp, "api-dirty")
	run(t, repo, "worktree", "add", "-q", "-b", "dirty", wt)
	write(t, filepath.Join(wt, "a.txt"), "changed\n")
	write(t, filepath.Join(wt, "staged.bin"), "\x00\x01binary")
	run(t, wt, "add", "staged.bin")
	if err := os.MkdirAll(filepath.Join(wt, "notes", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "notes", "todo.md"), "todo\n")
	if err := os.MkdirAll(filepath.Join(wt, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "node_modules", "dep.js"), "x")

	dir := filepath.Join(tmp, "backups", "20260930-120000", "api-dirty")
	if err := (git.Worktrees{}).Backup(context.Background(), domain.Worktree{Repo: repo, Path: wt, Branch: "dirty"}, dir); err != nil {
		t.Fatal(err)
	}

	names := tarNames(t, filepath.Join(dir, "untracked.tar"))
	if strings.Join(names, ",") != "notes/todo.md" {
		t.Errorf("untracked.tar = %v, want only notes/todo.md", names)
	}
	status, err := os.ReadFile(filepath.Join(dir, "status.txt"))
	if err != nil || !strings.Contains(string(status), wt) || !strings.Contains(string(status), "dirty") {
		t.Errorf("status.txt = %q, %v", status, err)
	}

	restore := filepath.Join(tmp, "api-restore")
	run(t, repo, "worktree", "add", "-q", "-b", "restore", restore)
	run(t, restore, "apply", "--binary", filepath.Join(dir, "patch.diff"))
	got, _ := os.ReadFile(filepath.Join(restore, "a.txt"))
	bin, _ := os.ReadFile(filepath.Join(restore, "staged.bin"))
	if string(got) != "changed\n" || string(bin) != "\x00\x01binary" {
		t.Errorf("patch restored a.txt %q, staged.bin %q", got, bin)
	}
}

func TestCleanupBackupOfACleanWorktreeHasNoTar(t *testing.T) {
	tmp, repo := clonedRepo(t)
	wt := filepath.Join(tmp, "api-clean")
	run(t, repo, "worktree", "add", "-q", "-b", "clean", wt)
	dir := filepath.Join(tmp, "b")
	if err := (git.Worktrees{}).Backup(context.Background(), domain.Worktree{Repo: repo, Path: wt, Branch: "clean"}, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "untracked.tar")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("untracked.tar: %v, want none", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "status.txt")); err != nil {
		t.Error(err)
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var names []string
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			names = append(names, strings.TrimPrefix(h.Name, "./"))
		}
	}
	sort.Strings(names)
	return names
}

func TestCleanupCreateBranchNeverMovesAnExistingOne(t *testing.T) {
	tmp, repo := clonedRepo(t)
	wt := filepath.Join(tmp, "api-x")
	run(t, repo, "worktree", "add", "-q", "--detach", wt)
	write(t, filepath.Join(wt, "d.txt"), "d\n")
	run(t, wt, "add", ".")
	run(t, wt, "commit", "-q", "-m", "d")
	head := cleanupGitOut(t, wt, "rev-parse", "HEAD")
	run(t, repo, "branch", "backup/wt-api-x", "main")
	taken := cleanupGitOut(t, repo, "rev-parse", "backup/wt-api-x")

	name, err := git.Worktrees{}.CreateBranch(context.Background(), domain.Worktree{Repo: repo, Path: wt}, "backup/wt-api-x")
	if err != nil {
		t.Fatal(err)
	}
	if name != "backup/wt-api-x-2" || cleanupGitOut(t, repo, "rev-parse", name) != head {
		t.Errorf("created %q, want backup/wt-api-x-2 at %s", name, head)
	}
	if cleanupGitOut(t, repo, "rev-parse", "backup/wt-api-x") != taken {
		t.Error("existing backup branch moved")
	}
}

func TestCleanupPruneDropsMovedWorktreeAndKeepsItsBranch(t *testing.T) {
	tmp, repo := clonedRepo(t)
	wt := filepath.Join(tmp, "api-gone")
	run(t, repo, "worktree", "add", "-q", "-b", "gone", wt)
	if err := os.Rename(wt, filepath.Join(tmp, "trashed")); err != nil {
		t.Fatal(err)
	}
	if err := (git.Worktrees{}).Prune(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if list := cleanupGitOut(t, repo, "worktree", "list"); strings.Contains(list, "api-gone") {
		t.Errorf("worktree list still has it:\n%s", list)
	}
	gitOut(t, repo, "rev-parse", "--verify", "refs/heads/gone")
}
