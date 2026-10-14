package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestReviewRangeFor(t *testing.T) {
	cases := []struct {
		name  string
		scope ReviewScope
		facts RangeFacts
		want  DiffRange
		err   error
	}{
		{"last turn diffs from the latest snapshot", ScopeLastTurn, RangeFacts{LatestTurn: "refs/crew/turns/s/k/3"}, DiffRange{From: "refs/crew/turns/s/k/3"}, nil},
		{"last turn without a prompt yet", ScopeLastTurn, RangeFacts{DefaultBranch: "main"}, DiffRange{}, ErrNoTurn},
		{"uncommitted diffs from HEAD", ScopeUncommitted, RangeFacts{LatestTurn: "x"}, DiffRange{From: "HEAD"}, nil},
		{"branch diffs from the merge base with origin", ScopeBranch, RangeFacts{DefaultBranch: "trunk"}, DiffRange{From: "origin/trunk", MergeBase: true}, nil},
		{"branch without a default branch", ScopeBranch, RangeFacts{}, DiffRange{}, ErrNoDefaultBranch},
		{"unknown scope", ReviewScope("everything"), RangeFacts{}, DiffRange{}, ErrUnknownScope},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RangeFor(c.scope, c.facts)
			if !errors.Is(err, c.err) || got != c.want {
				t.Errorf("RangeFor = %+v, %v; want %+v, %v", got, err, c.want, c.err)
			}
		})
	}
}

func TestReviewScopeShift(t *testing.T) {
	cases := []struct {
		from  ReviewScope
		delta int
		want  ReviewScope
	}{
		{ScopeLastTurn, 1, ScopeUncommitted},
		{ScopeUncommitted, 1, ScopeBranch},
		{ScopeBranch, 1, ScopeLastTurn},
		{ScopeLastTurn, -1, ScopeBranch},
		{ScopeBranch, -1, ScopeUncommitted},
		{ReviewScope("bogus"), 1, ScopeUncommitted},
	}
	for _, c := range cases {
		if got := c.from.Shift(c.delta); got != c.want {
			t.Errorf("%q.Shift(%d) = %q, want %q", c.from, c.delta, got, c.want)
		}
	}
}

func TestReviewTurnRefs(t *testing.T) {
	wt := "/src/api-feat"
	other := "/src/web-feat"
	key := WorktreeKey(wt)
	if key == WorktreeKey(other) || key != WorktreeKey(wt+"/") {
		t.Fatalf("WorktreeKey must tell worktrees apart and ignore a trailing slash: %q %q", key, WorktreeKey(other))
	}
	ref := TurnRef("s1", wt, 7)
	if ref != "refs/crew/turns/s1/"+key+"/7" {
		t.Fatalf("TurnRef = %q", ref)
	}
	if got := TurnRef("a/b c", wt, 1); strings.ContainsAny(strings.TrimPrefix(got, "refs/crew/turns/"), " ~^:?*[\\") || strings.Count(got, "/") != 5 {
		t.Errorf("TurnRef with an unsafe session id = %q", got)
	}

	refs := []string{
		TurnRef("s1", wt, 2),
		TurnRef("s1", wt, 10),
		TurnRef("s1", wt, 9),
		TurnRef("s2", wt, 30),
		TurnRef("s1", other, 40),
		"refs/crew/turns/s1/" + key + "/junk",
		"refs/heads/main",
	}
	latest, n := LatestTurn(refs, "s1", wt)
	if latest != TurnRef("s1", wt, 10) || n != 10 {
		t.Errorf("LatestTurn = %q, %d", latest, n)
	}
	if latest, n := LatestTurn(refs, "s3", wt); latest != "" || n != 0 {
		t.Errorf("LatestTurn with no turns = %q, %d", latest, n)
	}
	older := OlderTurns(refs, "s1", wt, 10)
	if want := []string{TurnRef("s1", wt, 2), TurnRef("s1", wt, 9)}; !reflect.DeepEqual(older, want) {
		t.Errorf("OlderTurns = %v, want %v", older, want)
	}
	of := TurnsOfWorktree(refs, wt)
	want := []string{TurnRef("s1", wt, 2), TurnRef("s1", wt, 10), TurnRef("s1", wt, 9), TurnRef("s2", wt, 30), "refs/crew/turns/s1/" + key + "/junk"}
	if !reflect.DeepEqual(of, want) {
		t.Errorf("TurnsOfWorktree = %v, want %v", of, want)
	}
}

const sampleDiff = `diff --git a/src/resolvers.ts b/src/resolvers.ts
index 1111111..2222222 100644
--- a/src/resolvers.ts
+++ b/src/resolvers.ts
@@ -41,4 +41,5 @@ export const publicResolvers
 export const publicResolvers = {
-  const orgId = ctx.user?.orgId;
+  const token = await verify(ctx);
+  if (!token) throw new Error();
   Query: {
 }
@@ -60 +61 @@
-a
\ No newline at end of file
+b
diff --git a/new.ts b/new.ts
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.ts
@@ -0,0 +1,2 @@
+one
+two
diff --git a/gone.ts b/gone.ts
deleted file mode 100644
index 4444444..0000000
--- a/gone.ts
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/old name.ts b/new name.ts
similarity 90%
rename from old name.ts
rename to new name.ts
index 5555555..6666666 100644
--- a/old name.ts
+++ b/new name.ts
@@ -1 +1 @@
-x
+y
diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..7777777
Binary files /dev/null and b/logo.png differ
diff --git a/same.sh b/same.sh
old mode 100644
new mode 100755
`

func TestReviewParseDiff(t *testing.T) {
	files := ParseDiff(sampleDiff)
	want := []FileDiff{
		{
			Path: "src/resolvers.ts", Status: FileModified, Added: 3, Deleted: 2, Blob: "2222222",
			Hunks: []Hunk{
				{Header: "@@ -41,4 +41,5 @@ export const publicResolvers", Lines: []DiffLine{
					{Kind: LineContext, Old: 41, New: 41, Text: "export const publicResolvers = {"},
					{Kind: LineDeleted, Old: 42, Text: "  const orgId = ctx.user?.orgId;"},
					{Kind: LineAdded, New: 42, Text: "  const token = await verify(ctx);"},
					{Kind: LineAdded, New: 43, Text: "  if (!token) throw new Error();"},
					{Kind: LineContext, Old: 43, New: 44, Text: "  Query: {"},
					{Kind: LineContext, Old: 44, New: 45, Text: "}"},
				}},
				{Header: "@@ -60 +61 @@", Lines: []DiffLine{
					{Kind: LineDeleted, Old: 60, Text: "a", NoEOL: true},
					{Kind: LineAdded, New: 61, Text: "b"},
				}},
			},
		},
		{Path: "new.ts", Status: FileAdded, Mode: "100644", Added: 2, Blob: "3333333", Hunks: []Hunk{{Header: "@@ -0,0 +1,2 @@", Lines: []DiffLine{
			{Kind: LineAdded, New: 1, Text: "one"},
			{Kind: LineAdded, New: 2, Text: "two"},
		}}}},
		{Path: "gone.ts", Status: FileDeleted, Mode: "100644", Deleted: 1, Blob: "0000000", Hunks: []Hunk{{Header: "@@ -1 +0,0 @@", Lines: []DiffLine{
			{Kind: LineDeleted, Old: 1, Text: "bye"},
		}}}},
		{Path: "new name.ts", OldPath: "old name.ts", Status: FileRenamed, Added: 1, Deleted: 1, Blob: "6666666", Hunks: []Hunk{{Header: "@@ -1 +1 @@", Lines: []DiffLine{
			{Kind: LineDeleted, Old: 1, Text: "x"},
			{Kind: LineAdded, New: 1, Text: "y"},
		}}}},
		{Path: "logo.png", Status: FileAdded, Mode: "100644", Binary: true, Blob: "7777777"},
		{Path: "same.sh", Status: FileModified},
	}
	if !reflect.DeepEqual(files, want) {
		for i := range max(len(files), len(want)) {
			var g, w FileDiff
			if i < len(files) {
				g = files[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("file %d:\n got %+v\nwant %+v", i, g, w)
			}
		}
	}
}

func TestReviewParseDiffEmpty(t *testing.T) {
	if got := ParseDiff(""); len(got) != 0 {
		t.Errorf("ParseDiff(\"\") = %+v", got)
	}
}

func TestReviewViewedResetsWhenTheFileChanges(t *testing.T) {
	marks := map[string]ViewedMark{}
	f := FileDiff{Path: "a.go", Blob: "aaa"}
	if IsViewed(marks, "/wt", f) {
		t.Fatal("nothing is viewed yet")
	}
	m := ViewedMark{Worktree: "/wt", Path: "a.go", Blob: "aaa"}
	marks[m.Key()] = m
	if !IsViewed(marks, "/wt", f) {
		t.Error("marked file is viewed")
	}
	if IsViewed(marks, "/other", f) {
		t.Error("a mark belongs to its worktree")
	}
	if IsViewed(marks, "/wt", FileDiff{Path: "b.go", Blob: "aaa"}) {
		t.Error("a mark belongs to its path")
	}
	if IsViewed(marks, "/wt", FileDiff{Path: "a.go", Blob: "bbb"}) {
		t.Error("a file that changed again is no longer viewed")
	}
	if (ViewedMark{Worktree: "/w", Path: "a/b"}).Key() == (ViewedMark{Worktree: "/w/a", Path: "b"}).Key() {
		t.Error("keys must not collide across worktree and path splits")
	}
}

func TestReviewSplitRows(t *testing.T) {
	del := func(n int, s string) *DiffLine { return &DiffLine{Kind: LineDeleted, Old: n, Text: s} }
	add := func(n int, s string) *DiffLine { return &DiffLine{Kind: LineAdded, New: n, Text: s} }
	ctx := func(o, n int, s string) *DiffLine { return &DiffLine{Kind: LineContext, Old: o, New: n, Text: s} }
	h := Hunk{Lines: []DiffLine{
		*ctx(1, 1, "a"),
		*del(2, "b"), *del(3, "c"), *add(2, "B"),
		*ctx(4, 3, "d"),
		*add(4, "e"),
		*del(5, "f"),
	}}
	want := []SplitRow{
		{Left: ctx(1, 1, "a"), Right: ctx(1, 1, "a")},
		{Left: del(2, "b"), Right: add(2, "B")},
		{Left: del(3, "c")},
		{Left: ctx(4, 3, "d"), Right: ctx(4, 3, "d")},
		{Right: add(4, "e")},
		{Left: del(5, "f")},
	}
	if got := SplitRows(h); !reflect.DeepEqual(got, want) {
		t.Errorf("SplitRows =\n%+v\nwant\n%+v", got, want)
	}
}
