package domain

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestReviewPromptMatchesTheGoldenFile(t *testing.T) {
	comments := []ReviewComment{
		CommentOn("/work/api-part-1", "src/resolvers.ts", []DiffLine{
			{Kind: LineAdded, New: 44, Text: "      const token = await verifyShareToken(ctx.shareToken);"},
			{Kind: LineAdded, New: 45, Text: "      if (!token) throw new ForbiddenError('invalid share token');"},
		}, "Check the token's expiry too."),
		CommentOn("/work/api-part-1", "src/resolvers.ts", []DiffLine{
			{Kind: LineDeleted, Old: 42, Text: "      const orgId = ctx.user?.orgId;"},
		}, "Keep this for the audit log.\nIt was used by the logger.\n"),
		CommentOn("/work/web-part-2", "src/ShareSheet.tsx", []DiffLine{
			{Kind: LineAdded, New: 1, Text: "export const ShareSheet = () => <Sheet />;"},
		}, "Needs a close button."),
	}
	want, err := os.ReadFile("testdata/review_prompt.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := ReviewPrompt(comments); got != string(want) {
		t.Errorf("prompt:\n%s\nwant:\n%s", got, want)
	}
}

func TestCommentOnAnchorsToTheNewSideUnlessEveryLineWasRemoved(t *testing.T) {
	cases := []struct {
		name  string
		lines []DiffLine
		want  ReviewComment
	}{
		{"mixed range keeps new numbers", []DiffLine{
			{Kind: LineDeleted, Old: 7, Text: "a"},
			{Kind: LineContext, Old: 8, New: 7, Text: "b"},
			{Kind: LineAdded, New: 8, Text: "c"},
		}, ReviewComment{Start: 7, End: 8, Code: []string{"a", "b", "c"}}},
		{"only removed lines use old numbers", []DiffLine{
			{Kind: LineDeleted, Old: 3, Text: "x"},
			{Kind: LineDeleted, Old: 4, Text: "y"},
		}, ReviewComment{Start: 3, End: 4, Removed: true, Code: []string{"x", "y"}}},
		{"one context line", []DiffLine{{Kind: LineContext, Old: 9, New: 10, Text: "z"}},
			ReviewComment{Start: 10, End: 10, Code: []string{"z"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CommentOn("/w", "f.go", c.lines, " note ")
			c.want.Worktree, c.want.Path, c.want.Body = "/w", "f.go", "note"
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestReviewPromptDedentsQuotedCodeByItsCommonIndent(t *testing.T) {
	c := CommentOn("/w", "f.go", []DiffLine{
		{Kind: LineAdded, New: 1, Text: "\t\tif x {"},
		{Kind: LineAdded, New: 2, Text: ""},
		{Kind: LineAdded, New: 3, Text: "\t\t\ty()"},
	}, "n")
	want := "Review comments on your changes. Each one names the worktree, file and lines it is about; make the edit in that worktree.\n\n" +
		"1. /w:f.go:1-3\n   > if x {\n   >\n   > \ty()\n   n\n"
	if got := ReviewPrompt([]ReviewComment{c}); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestDraftIsSentOnlyWhenTheSessionIsBetweenTools(t *testing.T) {
	now := time.Unix(100, 0)
	d := ReviewDraft{ID: "d1", Session: "s1"}.Add(ReviewComment{Worktree: "/w", Path: "a", Start: 1, End: 1, Body: "x"})
	if d.Status != DraftOpen || len(d.Comments) != 1 {
		t.Fatalf("draft %+v", d)
	}
	for _, st := range []AgentState{StateRunning, StateIdle, StateDone, StateWaiting} {
		t.Run(string(st), func(t *testing.T) {
			if _, _, ok := d.Dispatch(Session{State: st}, now); ok {
				t.Fatal("an open draft is sent before S")
			}
			q := d.Queue()
			next, prompt, ok := q.Dispatch(Session{State: st}, now)
			if st == StateRunning {
				if ok || next.Status != DraftQueued {
					t.Fatalf("sent while running: %+v", next)
				}
				return
			}
			if !ok || next.Status != DraftSent || !next.SentAt.Equal(now) || prompt != ReviewPrompt(d.Comments) {
				t.Fatalf("next %+v ok %v prompt %q", next, ok, prompt)
			}
			if _, _, again := next.Dispatch(Session{State: st}, now); again {
				t.Error("a sent draft is sent again")
			}
		})
	}
}

func TestDraftPromptEndsWithTheOverallNote(t *testing.T) {
	c := ReviewComment{Worktree: "/w", Path: "a.go", Start: 1, End: 1, Body: "x"}
	d := ReviewDraft{ID: "d1", Session: "s1"}.Add(c).WithNote("  Also add tests.\nKeep it small.\n ").Queue()
	_, prompt, ok := d.Dispatch(Session{State: StateIdle}, time.Unix(1, 0))
	want := ReviewPrompt([]ReviewComment{c}) + "\nOverall:\n   Also add tests.\n   Keep it small.\n"
	if !ok || prompt != want {
		t.Errorf("prompt %q\nwant %q", prompt, want)
	}
}

func TestBlankNoteLeavesThePromptAsTheComments(t *testing.T) {
	c := ReviewComment{Worktree: "/w", Path: "a.go", Start: 1, End: 1, Body: "x"}
	d := ReviewDraft{ID: "d1"}.Add(c).WithNote(" \n").Queue()
	if d.Note != "" {
		t.Errorf("note %q", d.Note)
	}
	if _, prompt, _ := d.Dispatch(Session{State: StateIdle}, time.Unix(1, 0)); prompt != ReviewPrompt(d.Comments) {
		t.Errorf("prompt %q", prompt)
	}
}

func TestEmptyDraftIsNeverQueued(t *testing.T) {
	if d := (ReviewDraft{ID: "d1"}).Queue(); d.Status != DraftOpen {
		t.Errorf("empty draft queued: %+v", d)
	}
}

func TestDraftLinksOnlyTheFirstTurnAfterItWasSent(t *testing.T) {
	d := ReviewDraft{Status: DraftSent}
	if !d.AwaitsTurn() {
		t.Fatal("a sent draft awaits its turn")
	}
	d = d.LinkTurn([]string{"refs/agentws/turns/s1/k/3"})
	if d.AwaitsTurn() || !reflect.DeepEqual(d.Turns, []string{"refs/agentws/turns/s1/k/3"}) {
		t.Errorf("linked %+v", d)
	}
	if (ReviewDraft{Status: DraftQueued}).AwaitsTurn() {
		t.Error("a queued draft has produced no turn yet")
	}
}

func TestHunkPatchRebuildsAPatchGitCanApply(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n" +
		"@@ -1,3 +1,3 @@ func x\n a\n-b\n+c\n d\n@@ -10 +10,2 @@\n z\n+y\n\\ No newline at end of file\n" +
		"diff --git a/n.go b/n.go\nnew file mode 100644\nindex 0000000..3\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1 @@\n+new\n" +
		"diff --git a/g.go b/g.go\ndeleted file mode 100644\nindex 4..0000000\n--- a/g.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n"
	files := ParseDiff(diff)
	cases := []struct {
		file, hunk int
		want       string
	}{
		{0, 0, "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@ func x\n a\n-b\n+c\n d\n"},
		{0, 1, "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -10 +10,2 @@\n z\n+y\n\\ No newline at end of file\n"},
		{1, 0, "diff --git a/n.go b/n.go\nnew file mode 100644\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1 @@\n+new\n"},
		{2, 0, "diff --git a/g.go b/g.go\ndeleted file mode 100644\n--- a/g.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n"},
	}
	for _, c := range cases {
		got, err := HunkPatch(files[c.file], files[c.file].Hunks[c.hunk])
		if err != nil || got != c.want {
			t.Errorf("file %d hunk %d: err %v\n%s\nwant\n%s", c.file, c.hunk, err, got, c.want)
		}
	}
}

func TestHunkPatchRefusesWhatItCannotRebuild(t *testing.T) {
	for _, f := range []FileDiff{
		{Path: "b", OldPath: "a", Status: FileRenamed, Hunks: []Hunk{{Header: "@@ -1 +1 @@"}}},
		{Path: "i.png", Binary: true},
		{Path: `"sp ace"`, Status: FileModified, Hunks: []Hunk{{Header: "@@ -1 +1 @@"}}},
	} {
		var h Hunk
		if len(f.Hunks) > 0 {
			h = f.Hunks[0]
		}
		if _, err := HunkPatch(f, h); err == nil {
			t.Errorf("%+v: want an error", f)
		}
	}
}
