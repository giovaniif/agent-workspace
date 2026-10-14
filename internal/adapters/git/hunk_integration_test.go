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
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.HunkGit = git.Review{}

const twentyLines = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nl20\n"

func twoHunkRepos(t *testing.T) (string, string) {
	t.Helper()
	a, b := reviewRepo(t), reviewRepo(t)
	for _, dir := range []string{a, b} {
		put(t, filepath.Join(dir, "f.txt"), twentyLines)
		gitOut(t, dir, "add", "f.txt")
		gitOut(t, dir, "commit", "-q", "-m", "f")
		edited := strings.Replace(strings.Replace(twentyLines, "l2\n", "two\n", 1), "l19\n", "nineteen\nextra\n", 1)
		put(t, filepath.Join(dir, "f.txt"), edited)
	}
	return a, b
}

func interactive(t *testing.T, dir, answers string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(answers)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func firstHunk(t *testing.T, dir string) (domain.FileDiff, domain.Hunk) {
	t.Helper()
	ctx := context.Background()
	tree, err := git.Review{}.WorkingTree(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := git.Review{}.Diff(ctx, dir, "HEAD", tree)
	if err != nil {
		t.Fatal(err)
	}
	files := domain.ParseDiff(out)
	if len(files) != 1 || len(files[0].Hunks) != 2 {
		t.Fatalf("want one file with two hunks, got %+v", files)
	}
	return files[0], files[0].Hunks[0]
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReviewStageHunkMatchesGitAddPatch(t *testing.T) {
	a, b := twoHunkRepos(t)
	interactive(t, a, "y\nn\n", "add", "-p", "f.txt")
	f, h := firstHunk(t, b)
	if err := app.ApplyHunk(context.Background(), git.Review{}, b, f, h, domain.HunkStage); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"diff", "--cached"}, {"diff"}} {
		if got, want := gitOut(t, b, args...), gitOut(t, a, args...); got != want {
			t.Errorf("git %v differs from git add -p:\n%s\nwant\n%s", args, got, want)
		}
	}
}

func TestReviewRevertHunkMatchesGitCheckoutPatchAndKeepsABackup(t *testing.T) {
	a, b := twoHunkRepos(t)
	interactive(t, a, "y\nn\n", "checkout", "-p", "--", "f.txt")
	f, h := firstHunk(t, b)
	if err := app.ApplyHunk(context.Background(), git.Review{}, b, f, h, domain.HunkRevert); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, filepath.Join(b, "f.txt")), read(t, filepath.Join(a, "f.txt")); got != want {
		t.Errorf("f.txt after revert:\n%s\nwant (git checkout -p)\n%s", got, want)
	}
	backups, _ := filepath.Glob(filepath.Join(b, ".git", "crew", "reverted", "*.patch"))
	if len(backups) != 1 || !strings.Contains(read(t, backups[0]), "+two") {
		t.Errorf("backups %v: want one patch holding the reverted lines", backups)
	}
}

func TestReviewRevertOfAStaleHunkChangesNothing(t *testing.T) {
	_, b := twoHunkRepos(t)
	f, h := firstHunk(t, b)
	path := filepath.Join(b, "f.txt")
	edited := strings.Replace(read(t, path), "two\n", "TWO\n", 1)
	put(t, path, edited)
	if err := app.ApplyHunk(context.Background(), git.Review{}, b, f, h, domain.HunkRevert); err == nil {
		t.Fatal("a hunk that no longer matches the file was reverted")
	}
	if got := read(t, path); got != edited {
		t.Errorf("f.txt changed:\n%s", got)
	}
	if backups, _ := filepath.Glob(filepath.Join(b, ".git", "crew", "reverted", "*.patch")); len(backups) != 0 {
		t.Errorf("a refused revert left backups %v", backups)
	}
}

func TestReviewStageAndRevertIgnoreInheritedGitLocationVariables(t *testing.T) {
	a, b := twoHunkRepos(t)
	interactive(t, a, "y\nn\n", "add", "-p", "f.txt")
	f, h := firstHunk(t, b)
	stray := filepath.Join(t.TempDir(), "stray-index")
	t.Setenv("GIT_INDEX_FILE", stray)
	t.Setenv("GIT_DIR", filepath.Join(a, ".git"))
	t.Setenv("GIT_WORK_TREE", a)
	if err := app.ApplyHunk(context.Background(), git.Review{}, b, f, h, domain.HunkStage); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if _, err := os.Stat(stray); err == nil {
		t.Error("staging wrote the inherited GIT_INDEX_FILE")
	}
	if got, want := gitOut(t, b, "diff", "--cached"), gitOut(t, a, "diff", "--cached"); got != want {
		t.Errorf("staged in b:\n%s\nwant\n%s", got, want)
	}
}

func TestReviewStageAHunkOfANewFileAddsIt(t *testing.T) {
	dir := reviewRepo(t)
	put(t, filepath.Join(dir, "n.txt"), "new\n")
	ctx := context.Background()
	tree, _ := git.Review{}.WorkingTree(ctx, dir)
	out, _ := git.Review{}.Diff(ctx, dir, "HEAD", tree)
	files := domain.ParseDiff(out)
	if len(files) != 1 {
		t.Fatalf("files %+v", files)
	}
	if err := app.ApplyHunk(ctx, git.Review{}, dir, files[0], files[0].Hunks[0], domain.HunkStage); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, dir, "diff", "--cached", "--name-status"); got != "A\tn.txt\n" {
		t.Errorf("staged %q", got)
	}
}
