//go:build integration

package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.ReviewGit = git.Review{}

func gitOut(tb testing.TB, dir string, args ...string) string {
	tb.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func put(tb testing.TB, path, body string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		tb.Fatal(err)
	}
}

func reviewRepo(tb testing.TB) string {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git not installed")
	}
	tb.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	tb.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	tmp, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	origin := filepath.Join(tmp, "origin.git")
	gitOut(tb, tmp, "init", "-q", "--bare", "-b", "main", origin)
	repo := filepath.Join(tmp, "api")
	gitOut(tb, tmp, "clone", "-q", origin, repo)
	put(tb, filepath.Join(repo, "a.txt"), "a\n")
	put(tb, filepath.Join(repo, "b.txt"), "b\n")
	gitOut(tb, repo, "add", ".")
	gitOut(tb, repo, "commit", "-q", "-m", "init")
	gitOut(tb, repo, "push", "-q", "origin", "HEAD:main")
	gitOut(tb, repo, "remote", "set-head", "origin", "main")
	return repo
}

func reviewPaths(tb testing.TB, r *app.Reviewer, scope domain.ReviewScope, session, dir string) []string {
	tb.Helper()
	got := r.Review(context.Background(), scope, []app.ReviewTarget{{
		Session: session, Worktree: domain.Worktree{ID: dir, Path: dir}, DefaultBranch: "main",
	}})
	if got[0].Err != "" {
		tb.Fatalf("%s review: %s", scope, got[0].Err)
	}
	var out []string
	for _, f := range got[0].Files {
		out = append(out, string(f.Status)+" "+f.Path)
	}
	return out
}

func TestReviewLastTurnShowsOnlyChangesAfterThePrompt(t *testing.T) {
	repo := reviewRepo(t)
	ctx := context.Background()
	g := git.Review{}
	put(t, filepath.Join(repo, "a.txt"), "a before the prompt\n")
	put(t, filepath.Join(repo, "early.txt"), "untracked before the prompt\n")
	statusBefore := gitOut(t, repo, "status", "--porcelain")

	if _, err := app.SnapshotTurn(ctx, g, "s1", repo); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Errorf("snapshot touched the index or worktree:\n%s\nwas\n%s", got, statusBefore)
	}
	put(t, filepath.Join(repo, "b.txt"), "b after the prompt\n")
	put(t, filepath.Join(repo, "late.txt"), "new after the prompt\n")

	r := app.NewReviewer(g)
	if got, want := reviewPaths(t, r, domain.ScopeLastTurn, "s1", repo), []string{"M b.txt", "A late.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("last turn = %v, want %v", got, want)
	}
	if got, want := reviewPaths(t, r, domain.ScopeUncommitted, "s1", repo), []string{"M a.txt", "M b.txt", "A early.txt", "A late.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("uncommitted = %v, want %v", got, want)
	}

	gitOut(t, repo, "checkout", "-q", "-b", "feat")
	gitOut(t, repo, "add", "a.txt")
	gitOut(t, repo, "commit", "-q", "-m", "a")
	if got, want := reviewPaths(t, r, domain.ScopeBranch, "s1", repo), []string{"M a.txt", "M b.txt", "A early.txt", "A late.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch = %v, want %v", got, want)
	}
	if got, want := reviewPaths(t, r, domain.ScopeUncommitted, "s1", repo), []string{"M b.txt", "A early.txt", "A late.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("uncommitted after a commit = %v, want %v", got, want)
	}
}

func TestReviewTurnRefsStayOutOfBranchesAndGoWithTheWorktree(t *testing.T) {
	repo := reviewRepo(t)
	ctx := context.Background()
	g := git.Review{}
	wt := filepath.Join(filepath.Dir(repo), "api-feat")
	gitOut(t, repo, "worktree", "add", "-q", "-b", "feat", wt)
	branches := gitOut(t, repo, "branch", "--all")
	put(t, filepath.Join(wt, "a.txt"), "edited\n")
	for range 2 {
		if _, err := app.SnapshotTurn(ctx, g, "s1", wt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.SnapshotTurn(ctx, g, "s1", repo); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, repo, "branch", "--all"); got != branches {
		t.Errorf("git branch changed:\n%s\nwas\n%s", got, branches)
	}
	refs := strings.Fields(gitOut(t, repo, "for-each-ref", "--format=%(refname)", "refs/agentws/"))
	want := []string{domain.TurnRef("s1", repo, 1), domain.TurnRef("s1", wt, 2)}
	sort.Strings(want)
	if !reflect.DeepEqual(refs, want) {
		t.Errorf("refs = %v, want %v", refs, want)
	}
	for _, r := range strings.Fields(gitOut(t, repo, "for-each-ref", "--format=%(refname)")) {
		if !strings.HasPrefix(r, "refs/heads/") && !strings.HasPrefix(r, "refs/remotes/") && !strings.HasPrefix(r, "refs/agentws/") {
			t.Errorf("unexpected ref %s", r)
		}
	}

	gitOut(t, repo, "worktree", "remove", "--force", wt)
	if err := app.DropTurns(ctx, g, repo, wt); err != nil {
		t.Fatal(err)
	}
	refs = strings.Fields(gitOut(t, repo, "for-each-ref", "--format=%(refname)", "refs/agentws/"))
	if want := []string{domain.TurnRef("s1", repo, 1)}; !reflect.DeepEqual(refs, want) {
		t.Errorf("after removal refs = %v, want %v", refs, want)
	}
}

func TestReviewSeesASameSizeEditRightAfterACommit(t *testing.T) {
	repo := reviewRepo(t)
	put(t, filepath.Join(repo, "a.txt"), "x\n")
	gitOut(t, repo, "commit", "-q", "-am", "x")
	put(t, filepath.Join(repo, "a.txt"), "y\n")
	time.Sleep(1100 * time.Millisecond)
	if got := reviewPaths(t, app.NewReviewer(git.Review{}), domain.ScopeUncommitted, "s1", repo); !reflect.DeepEqual(got, []string{"M a.txt"}) {
		t.Fatalf("uncommitted = %v, want the same-size edit", got)
	}
}

func TestReviewBlobIsTheFullHashWhateverCoreAbbrev(t *testing.T) {
	repo := reviewRepo(t)
	gitOut(t, repo, "config", "core.abbrev", "5")
	put(t, filepath.Join(repo, "a.txt"), "changed\n")
	got := app.NewReviewer(git.Review{}).Review(context.Background(), domain.ScopeUncommitted, []app.ReviewTarget{{
		Session: "s1", Worktree: domain.Worktree{ID: repo, Path: repo},
	}})
	want := strings.TrimSpace(gitOut(t, repo, "hash-object", "a.txt"))
	if len(got[0].Files) != 1 || got[0].Files[0].Blob != want {
		t.Fatalf("review = %+v, want blob %s", got[0], want)
	}
}

func BenchmarkReviewOpen50Files(b *testing.B) {
	repo := reviewRepo(b)
	const files, lines = 50, 60
	body := func(tag string) string {
		var s strings.Builder
		for i := range lines {
			fmt.Fprintf(&s, "func f%d() int { return %d } // %s\n", i, i, tag)
		}
		return s.String()
	}
	for i := range files {
		put(b, filepath.Join(repo, "src", fmt.Sprintf("file%02d.go", i)), body("old"))
	}
	gitOut(b, repo, "add", ".")
	gitOut(b, repo, "commit", "-q", "-m", "files")
	for i := range files {
		put(b, filepath.Join(repo, "src", fmt.Sprintf("file%02d.go", i)), body("new"))
	}
	targets := []app.ReviewTarget{{Session: "s1", Worktree: domain.Worktree{ID: repo, Path: repo}}}
	b.ResetTimer()
	start := time.Now()
	for range b.N {
		got := app.NewReviewer(git.Review{}).Review(context.Background(), domain.ScopeUncommitted, targets)
		changed := 0
		for _, f := range got[0].Files {
			changed += f.Added + f.Deleted
		}
		if got[0].Err != "" || len(got[0].Files) != files || changed != 2*files*lines {
			b.Fatalf("review: %d files, %d lines, err %q", len(got[0].Files), changed, got[0].Err)
		}
	}
	perOp := time.Since(start) / time.Duration(b.N)
	if perOp > 300*time.Millisecond {
		b.Fatalf("opening a %d-file review took %v, budget 300ms", files, perOp)
	}
}
