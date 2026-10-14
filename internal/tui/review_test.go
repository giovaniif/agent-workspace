package tui_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

const resolversDiff = `diff --git a/src/graphql/public/resolvers.ts b/src/graphql/public/resolvers.ts
index 1111111..b1b1b1b 100644
--- a/src/graphql/public/resolvers.ts
+++ b/src/graphql/public/resolvers.ts
@@ -41,6 +41,7 @@ export const publicResolvers
 export const publicResolvers = {
   Query: {
     sharedProject: async (_, { id }, ctx) => {
-      const orgId = ctx.user?.orgId;
-      return ctx.db.project.findFirst({ where: { id } });
+      const token = await verifyShareToken(ctx.shareToken);
+      if (!token) throw new ForbiddenError('invalid share token');
+      return ctx.db.project.findFirst({ where: { id, orgId: token.orgId } });
     },
   },
diff --git a/src/graphql/public/resolvers.test.ts b/src/graphql/public/resolvers.test.ts
new file mode 100644
index 0000000..c2c2c2c
--- /dev/null
+++ b/src/graphql/public/resolvers.test.ts
@@ -0,0 +1,2 @@
+import { test } from 'bun:test';
+test('scopes by org', () => {});
`

const shareSheetDiff = `diff --git a/src/ShareSheet.tsx b/src/ShareSheet.tsx
index 3333333..d4d4d4d 100644
--- a/src/ShareSheet.tsx
+++ b/src/ShareSheet.tsx
@@ -1,2 +1,2 @@
-export const ShareSheet = () => null;
+export const ShareSheet = () => <Sheet />;
 export default ShareSheet;
`

func reviewFixture() (rpc.State, *fakeReviewer) {
	st := fixture(1, 2)
	api, web := st.Worktrees[0], st.Worktrees[1]
	return st, &fakeReviewer{reply: rpc.Review{Worktrees: []domain.WorktreeReview{
		{Worktree: api, Files: domain.ParseDiff(resolversDiff)},
		{Worktree: web, Files: domain.ParseDiff(shareSheetDiff)},
	}}}
}

func reviewModel(t testing.TB, width, height int) (tui.Model, *fakeReviewer) {
	st, rv := reviewFixture()
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = update(m, tui.StateMsg(st))
	return m, rv
}

func drive(m tui.Model, msgs ...tea.Msg) tui.Model {
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next.(tui.Model)
		pending := []tea.Cmd{cmd}
		for len(pending) > 0 {
			c := pending[0]
			pending = pending[1:]
			if c == nil {
				continue
			}
			switch out := c().(type) {
			case nil:
			case tea.BatchMsg:
				pending = append(pending, out...)
			default:
				next, cmd := m.Update(out)
				m = next.(tui.Model)
				pending = append(pending, cmd)
			}
		}
	}
	return m
}

func keys(ks ...string) []tea.Msg {
	out := make([]tea.Msg, len(ks))
	for i, k := range ks {
		out[i] = key(k)
	}
	return out
}

func TestReviewOpensForTheSelectedSessionAndCollapsesTheSidebar(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, key("r"))
	if !reflect.DeepEqual(rv.layouts, []bool{true}) {
		t.Errorf("layout calls = %v, want the sidebar widened", rv.layouts)
	}
	if want := []rpc.ReviewParams{{Session: "s01", Scope: domain.ScopeUncommitted}}; !reflect.DeepEqual(rv.asked, want) {
		t.Errorf("asked %+v, want %+v", rv.asked, want)
	}
	out := screen(m)
	for _, want := range []string{"REVIEW", "last turn", "uncommitted", "branch vs base", "3 files", "+6", "-3", "0/3 viewed",
		"worktree", "all", "api:part-1 #3600", "web:part-2", "resolvers.ts", "resolvers.test.ts", "ShareSheet.tsx",
		"src/graphql/public/resolvers.ts", "@@ -41,6 +41,7 @@", "verifyShareToken"} {
		if !strings.Contains(out, want) {
			t.Errorf("review screen lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SESSIONS") {
		t.Errorf("the sidebar should collapse to a rail:\n%s", out)
	}

	m = drive(m, key("r"))
	if !reflect.DeepEqual(rv.layouts, []bool{true, false}) {
		t.Errorf("layout calls = %v, want the sidebar put back", rv.layouts)
	}
	if !strings.Contains(screen(m), "SESSIONS") {
		t.Errorf("closing the review shows the sidebar again:\n%s", screen(m))
	}
}

func TestReviewScopeAndWorktreeKeysAskAgain(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	drive(m, keys("r", "]", "]", "[", "w", "w", "w")...)
	want := []rpc.ReviewParams{
		{Session: "s01", Scope: domain.ScopeUncommitted},
		{Session: "s01", Scope: domain.ScopeBranch},
		{Session: "s01", Scope: domain.ScopeLastTurn},
		{Session: "s01", Scope: domain.ScopeBranch},
		{Session: "s01", Scope: domain.ScopeBranch, Worktree: "w01-0"},
		{Session: "s01", Scope: domain.ScopeBranch, Worktree: "w01-1"},
		{Session: "s01", Scope: domain.ScopeBranch},
	}
	if !reflect.DeepEqual(rv.asked, want) {
		t.Errorf("asked\n%+v\nwant\n%+v", rv.asked, want)
	}
}

func TestReviewViewedMarkTogglesAndResetsWhenTheFileChanges(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "v")...)
	want := []viewedCall{{domain.ViewedMark{Worktree: "w01-0", Path: "src/graphql/public/resolvers.ts", Blob: "b1b1b1b"}, true}}
	if !reflect.DeepEqual(rv.marked, want) {
		t.Fatalf("marked %+v, want %+v", rv.marked, want)
	}
	if out := screen(m); !strings.Contains(out, "1/3 viewed") || !strings.Contains(out, "✓") {
		t.Errorf("a viewed file shows its mark:\n%s", out)
	}
	if strings.Contains(screen(drive(m, key("n"))), "src/graphql/public/resolvers.ts ") {
		t.Error("n moves to the next file")
	}

	rv.reply.Viewed = []domain.ViewedMark{want[0].mark}
	m = drive(m, key("]"), key("["))
	if !strings.Contains(screen(m), "1/3 viewed") {
		t.Errorf("the daemon's mark still applies to the same content:\n%s", screen(m))
	}
	rv.reply.Worktrees[0].Files[0].Blob = "e5e5e5e"
	m = drive(m, key("]"), key("["))
	if out := screen(m); !strings.Contains(out, "0/3 viewed") || strings.Contains(out, "✓") {
		t.Errorf("a file that changed again is no longer viewed:\n%s", out)
	}

	drive(m, key("v"), key("v"))
	if last := rv.marked[len(rv.marked)-1]; last.viewed {
		t.Errorf("v twice clears the mark; last call %+v", last)
	}
}

func TestReviewSplitViewPutsOldAndNewSideBySide(t *testing.T) {
	m, _ := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "u")...)
	var row string
	for _, line := range strings.Split(screen(m), "\n") {
		if strings.Contains(line, "const orgId") {
			row = line
		}
	}
	if !strings.Contains(row, "const token") {
		t.Errorf("split row pairs the deletion with its addition: %q", row)
	}
	m = drive(m, key("u"))
	for _, line := range strings.Split(screen(m), "\n") {
		if strings.Contains(line, "const orgId") && strings.Contains(line, "const token") {
			t.Errorf("unified view shows one side per row: %q", line)
		}
	}
}

func TestReviewSplitContextShowsEachSidesLineNumber(t *testing.T) {
	m, _ := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "u")...)
	found := false
	for _, line := range strings.Split(screen(m), "\n") {
		if strings.Count(line, "      },") == 2 {
			found = true
			if !strings.Contains(line, "46    ") || !strings.Contains(line, "47    ") {
				t.Errorf("a context row carries the old number left and the new one right: %q", line)
			}
			break
		}
	}
	if !found {
		t.Errorf("no split context row for },:\n%s", screen(m))
	}
}

func TestReviewLabelsNameTheRepoNotItsPath(t *testing.T) {
	st, rv := reviewFixture()
	for i := range st.Worktrees {
		st.Worktrees[i].Repo = "/src/shop/" + st.Worktrees[i].Repo
	}
	rv.reply.Worktrees[0].Worktree = st.Worktrees[0]
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: 150, Height: 40})
	m = drive(m, tui.StateMsg(st), key("r"))
	out := screen(m)
	if strings.Contains(out, "/src/shop") || !strings.Contains(out, "api:part-1 #3600") {
		t.Errorf("labels should use the repo's name:\n%s", out)
	}
}

func TestReviewAFailedFetchShowsTheErrorNotTheOldScopesFiles(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, key("r"))
	rv.err = errors.New("daemon timed out")
	out := screen(drive(m, key("]")))
	if !strings.Contains(out, "daemon timed out") || strings.Contains(out, "resolvers.ts") {
		t.Errorf("a failed fetch must show its error and drop the old files:\n%s", out)
	}
}

func TestReviewBranchScopeNamesTheDefaultBranchWhenItIsKnown(t *testing.T) {
	st, rv := reviewFixture()
	st.Workspaces = []domain.Workspace{{Root: "/src", Repos: []domain.Repo{
		{Name: "api", Path: "api", DefaultBranch: "trunk"},
		{Name: "web", Path: "web", DefaultBranch: "trunk"},
	}}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: 150, Height: 40})
	m = drive(m, tui.StateMsg(st), key("r"))
	if out := screen(m); !strings.Contains(out, "branch vs trunk") {
		t.Errorf("the scope should name the default branch:\n%s", out)
	}
	st.Workspaces[0].Repos[1].DefaultBranch = "main"
	m = drive(m, tui.StateMsg(st))
	if out := screen(m); !strings.Contains(out, "branch vs base") {
		t.Errorf("repos that disagree fall back to base:\n%s", out)
	}
}

func TestReviewWithoutAReviewerDoesNothing(t *testing.T) {
	st := fixture(1, 1)
	m := drive(newModel(&st, nil), key("r"))
	if !strings.Contains(screen(m), "SESSIONS") {
		t.Error("r without a reviewer keeps the sidebar")
	}
}

func TestGoldenReview(t *testing.T) {
	m, _ := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "v")...)
	golden.RequireEqual(t, screen(m))
}

func BenchmarkReviewScroll(b *testing.B) {
	st := fixture(1, 1)
	var diff strings.Builder
	for f := range 50 {
		fmt.Fprintf(&diff, "diff --git a/src/file%02d.go b/src/file%02d.go\nindex 1..2 100644\n--- a/src/file%02d.go\n+++ b/src/file%02d.go\n@@ -1,30 +1,30 @@\n", f, f, f, f)
		for i := range 30 {
			fmt.Fprintf(&diff, "-func f%d() int { return %d } // old\n+func f%d() int { return %d } // new\n", i, i, i, i)
		}
	}
	rv := &fakeReviewer{reply: rpc.Review{Worktrees: []domain.WorktreeReview{{Worktree: st.Worktrees[0], Files: domain.ParseDiff(diff.String())}}}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: 200, Height: 60})
	m = update(m, tui.StateMsg(st))
	m = drive(m, key("r"))
	frames := []tea.KeyPressMsg{key("j"), key("j"), key("j"), key("n"), key("u"), key("k"), key("p"), key("u")}
	samples := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		m = update(m, frames[i%len(frames)])
		_ = m.View()
		samples = append(samples, time.Since(start))
	}
	b.StopTimer()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[len(samples)*95/100]
	b.ReportMetric(float64(p95.Microseconds())/1000, "p95-ms")
	if len(samples) >= 100 && p95 > 16*time.Millisecond {
		b.Fatalf("review frame p95 %v exceeds the 16ms budget", p95)
	}
}

func agentColumnModel(t *testing.T, width int) tui.Model {
	t.Helper()
	st, rv := reviewFixture()
	st.Events = []domain.SessionEvent{
		{SessionID: "s01", Kind: domain.EventPreToolUse, Tool: "Edit", Detail: "resolvers.ts"},
		{SessionID: "s01", Kind: domain.EventPreToolUse, Tool: "Bash", Detail: "bun test"},
		{SessionID: "s01", Kind: domain.EventStop, Text: "Org scoping now comes from the share token."},
	}
	st.Drafts = []domain.ReviewDraft{{ID: "d1", Session: "s01", Status: domain.DraftOpen, Comments: []domain.ReviewComment{
		{Worktree: st.Worktrees[0].ID, Path: "a.ts", Start: 1, Body: "x"},
		{Worktree: st.Worktrees[1].ID, Path: "b.ts", Start: 2, Body: "y"},
	}}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: width, Height: 40})
	m = update(m, tui.StateMsg(st))
	return drive(m, keys("r")...)
}

func TestReviewShowsTheAgentColumnBesideTheDiff(t *testing.T) {
	out := screen(agentColumnModel(t, 180))
	for _, want := range []string{"Edit resolvers.ts", "bun test", "Org scoping now comes", "Draft review → this session", "2 comments · 2 worktrees"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the review:\n%s", want, out)
		}
	}
}

func TestReviewLeavesTheAgentColumnOutWhenNarrow(t *testing.T) {
	if out := screen(agentColumnModel(t, 110)); strings.Contains(out, "Draft review") {
		t.Fatalf("the agent column is drawn at 110 columns:\n%s", out)
	}
}
