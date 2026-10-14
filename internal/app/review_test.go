package app_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

const oneFileDiff = "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n"

func paths(files []domain.FileDiff) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestReviewSnapshotTurnKeepsOnlyTheNewest(t *testing.T) {
	g := newFakeReviewGit()
	ctx := context.Background()
	g.trees["/wt"] = "tree1"
	first, err := app.SnapshotTurn(ctx, g, "s1", "/wt")
	if err != nil || first != domain.TurnRef("s1", "/wt", 1) {
		t.Fatalf("first snapshot = %q, %v", first, err)
	}
	g.trees["/wt"] = "tree2"
	second, err := app.SnapshotTurn(ctx, g, "s1", "/wt")
	if err != nil || second != domain.TurnRef("s1", "/wt", 2) {
		t.Fatalf("second snapshot = %q, %v", second, err)
	}
	if want := map[string]string{second: "tree2"}; !reflect.DeepEqual(g.refs["/wt"], want) {
		t.Errorf("refs = %v, want %v", g.refs["/wt"], want)
	}
}

func TestReviewSnapshotTurnFailsOutsideARepo(t *testing.T) {
	if _, err := app.SnapshotTurn(context.Background(), newFakeReviewGit(), "s1", "/nowhere"); err == nil {
		t.Fatal("want an error")
	}
}

func TestReviewDropTurnsRemovesOnlyThatWorktreesRefs(t *testing.T) {
	g := newFakeReviewGit()
	keep := domain.TurnRef("s1", "/repo/other", 4)
	g.refs["/repo"] = map[string]string{
		domain.TurnRef("s1", "/repo/gone", 1): "t",
		domain.TurnRef("s2", "/repo/gone", 3): "t",
		keep:                                  "t",
		"refs/heads/main":                     "t",
	}
	if err := app.DropTurns(context.Background(), g, "/repo", "/repo/gone"); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{keep: "t", "refs/heads/main": "t"}; !reflect.DeepEqual(g.refs["/repo"], want) {
		t.Errorf("refs = %v, want %v", g.refs["/repo"], want)
	}
}

func TestReviewScopesPickTheirBase(t *testing.T) {
	g := newFakeReviewGit()
	g.trees["/wt"] = "now"
	g.revs["/wt HEAD"] = "head"
	g.mergeBases["/wt origin/main"] = "base"
	g.refs["/wt"] = map[string]string{domain.TurnRef("s1", "/wt", 2): "turn2"}
	g.diffs["turn2..now"] = oneFileDiff
	g.diffs["head..now"] = oneFileDiff + "diff --git a/b.go b/b.go\nnew file mode 100644\nindex 0..3\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1 @@\n+z\n"
	g.diffs["base..now"] = g.diffs["head..now"] + "diff --git a/c.go b/c.go\nindex 4..5 100644\n--- a/c.go\n+++ b/c.go\n@@ -1 +1 @@\n-p\n+q\n"
	target := app.ReviewTarget{Session: "s1", Worktree: domain.Worktree{ID: "/wt", Path: "/wt"}, DefaultBranch: "main"}
	cases := []struct {
		scope domain.ReviewScope
		want  []string
	}{
		{domain.ScopeLastTurn, []string{"a.go"}},
		{domain.ScopeUncommitted, []string{"a.go", "b.go"}},
		{domain.ScopeBranch, []string{"a.go", "b.go", "c.go"}},
	}
	r := app.NewReviewer(g)
	for _, c := range cases {
		got := r.Review(context.Background(), c.scope, []app.ReviewTarget{target})
		if len(got) != 1 || got[0].Err != "" || !reflect.DeepEqual(paths(got[0].Files), c.want) {
			t.Errorf("%s: review = %+v, want files %v", c.scope, got, c.want)
		}
	}
}

func TestReviewNamesTheCommitEachScopeStartsFrom(t *testing.T) {
	g := newFakeReviewGit()
	g.trees["/wt"] = "now"
	g.revs["/wt HEAD"] = "head"
	g.mergeBases["/wt origin/main"] = "base"
	g.refs["/wt"] = map[string]string{domain.TurnRef("s1", "/wt", 2): "turn2"}
	target := app.ReviewTarget{Session: "s1", Worktree: domain.Worktree{ID: "/wt", Path: "/wt"}, DefaultBranch: "main"}
	r := app.NewReviewer(g)
	for scope, want := range map[domain.ReviewScope]string{domain.ScopeLastTurn: "turn2", domain.ScopeUncommitted: "head", domain.ScopeBranch: "base"} {
		g.diffs[want+"..now"] = oneFileDiff
		got := r.Review(context.Background(), scope, []app.ReviewTarget{target})
		if len(got) != 1 || got[0].From != want {
			t.Errorf("%s: From = %+v, want %q", scope, got, want)
		}
	}
	got := r.Review(context.Background(), domain.ScopeUncommitted, []app.ReviewTarget{target})
	if got[0].From != "head" {
		t.Errorf("a cached review lost its From: %+v", got)
	}
}

func TestReviewErrorsStayWithTheirWorktree(t *testing.T) {
	g := newFakeReviewGit()
	g.trees["/ok"] = "now"
	g.revs["/ok HEAD"] = "head"
	g.diffs["head..now"] = oneFileDiff
	r := app.NewReviewer(g)
	got := r.Review(context.Background(), domain.ScopeLastTurn, []app.ReviewTarget{
		{Session: "s1", Worktree: domain.Worktree{ID: "/ok", Path: "/ok"}},
	})
	if len(got) != 1 || got[0].Err != domain.ErrNoTurn.Error() {
		t.Errorf("last turn without a snapshot = %+v", got)
	}
	got = r.Review(context.Background(), domain.ScopeUncommitted, []app.ReviewTarget{
		{Session: "s1", Worktree: domain.Worktree{ID: "/gone", Path: "/gone"}},
		{Session: "s1", Worktree: domain.Worktree{ID: "/ok", Path: "/ok"}},
	})
	if len(got) != 2 || got[0].Err == "" || got[0].Worktree.ID != "/gone" || got[1].Err != "" || len(got[1].Files) != 1 {
		t.Errorf("one broken worktree = %+v", got)
	}
}

func TestReviewDiffsAreCachedByTreeHash(t *testing.T) {
	g := newFakeReviewGit()
	g.trees["/wt"] = "t1"
	g.revs["/wt HEAD"] = "head"
	g.diffs["head..t1"] = oneFileDiff
	g.diffs["head..t2"] = oneFileDiff
	r := app.NewReviewer(g)
	targets := []app.ReviewTarget{{Session: "s1", Worktree: domain.Worktree{ID: "/wt", Path: "/wt"}}}
	r.Review(context.Background(), domain.ScopeUncommitted, targets)
	r.Review(context.Background(), domain.ScopeUncommitted, targets)
	if g.diffCalls != 1 {
		t.Errorf("same tree ran git diff %d times, want 1", g.diffCalls)
	}
	g.trees["/wt"] = "t2"
	r.Review(context.Background(), domain.ScopeUncommitted, targets)
	if g.diffCalls != 2 {
		t.Errorf("a changed tree must diff again; calls = %d", g.diffCalls)
	}
	g.revs["/wt HEAD"] = "head2"
	r.Review(context.Background(), domain.ScopeUncommitted, targets)
	if g.diffCalls != 3 {
		t.Errorf("a moved base must diff again; calls = %d", g.diffCalls)
	}
}
