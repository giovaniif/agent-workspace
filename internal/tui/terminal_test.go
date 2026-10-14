package tui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func terminalModel(t testing.TB, width, height int) (tui.Model, *fakeCaller, *fakeReviewer) {
	st, rv := reviewFixture()
	calls := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv, Calls: calls})
	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = update(m, tui.StateMsg(st))
	return m, calls, rv
}

func TestShellNvimKeysCallTheDaemonForTheSelectedSession(t *testing.T) {
	m, calls, _ := terminalModel(t, 150, 40)
	drive(m, keys("t", "T", "e")...)
	want := []call{
		{rpc.MethodShellToggle, rpc.ShellParams{Session: "s01"}},
		{rpc.MethodShellToggle, rpc.ShellParams{Session: "s01", Popup: true}},
		{rpc.MethodNvimToggle, rpc.NvimParams{Session: "s01"}},
	}
	if !reflect.DeepEqual(calls.calls, want) {
		t.Fatalf("calls = %+v; want %+v", calls.calls, want)
	}
}

func TestShellUsesTheWorktreeTheReviewLastFocused(t *testing.T) {
	m, calls, _ := terminalModel(t, 150, 40)
	drive(m, keys("r", "w", "r", "t")...)
	want := call{rpc.MethodShellToggle, rpc.ShellParams{Session: "s01", Worktree: "w01-0"}}
	if got := calls.calls[len(calls.calls)-1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("last call = %+v; want %+v", got, want)
	}
}

func TestShellNvimKeysNeedACaller(t *testing.T) {
	st, rv := reviewFixture()
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tui.StateMsg(st))
	if _, cmd := m.Update(key("t")); cmd != nil {
		t.Fatal("t did something without a caller")
	}
	if _, cmd := update(tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: &fakeCaller{}}), tea.WindowSizeMsg{Width: 100, Height: 30}).Update(key("e")); cmd != nil {
		t.Fatal("e did something with no session selected")
	}
}

func TestReviewOOpensTheFileAtTheCursorLineInNvimAndClosesTheReview(t *testing.T) {
	cases := []struct {
		name   string
		height int
		keys   []string
		want   rpc.NvimParams
	}{
		{"the first line of the first file", 40, []string{"r", "o"}, rpc.NvimParams{Session: "s01", Worktree: "w01-0", Path: "src/graphql/public/resolvers.ts", Line: 41}},
		{"a deleted line opens at the next line that exists", 8, []string{"r", "j", "j", "j", "j", "o"}, rpc.NvimParams{Session: "s01", Worktree: "w01-0", Path: "src/graphql/public/resolvers.ts", Line: 44}},
		{"a context line opens at its new line number", 8, []string{"r", "j", "j", "o"}, rpc.NvimParams{Session: "s01", Worktree: "w01-0", Path: "src/graphql/public/resolvers.ts", Line: 43}},
		{"the split view reads the new side", 8, []string{"r", "u", "j", "j", "j", "j", "o"}, rpc.NvimParams{Session: "s01", Worktree: "w01-0", Path: "src/graphql/public/resolvers.ts", Line: 45}},
		{"a file in another worktree", 40, []string{"r", "n", "n", "o"}, rpc.NvimParams{Session: "s01", Worktree: "w01-1", Path: "src/ShareSheet.tsx", Line: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, calls, rv := terminalModel(t, 150, c.height)
			m = drive(m, keys(c.keys...)...)
			want := []call{{rpc.MethodNvimOpen, c.want}}
			if !reflect.DeepEqual(calls.calls, want) {
				t.Fatalf("calls = %+v; want %+v", calls.calls, want)
			}
			if !reflect.DeepEqual(rv.layouts, []bool{true, false}) {
				t.Fatalf("layout calls = %v; want the review closed so nvim has room", rv.layouts)
			}
			if !strings.Contains(screen(m), "SESSIONS") {
				t.Fatalf("the sidebar did not come back:\n%s", screen(m))
			}
		})
	}
}

func TestReviewOWithoutFilesDoesNothing(t *testing.T) {
	m, calls, rv := terminalModel(t, 150, 40)
	rv.reply.Worktrees = nil
	m = drive(m, keys("r", "o")...)
	if len(calls.calls) != 0 || !strings.Contains(screen(m), "REVIEW") {
		t.Fatalf("calls %+v; o with nothing to open should leave the review alone", calls.calls)
	}
}

func TestReviewFooterOffersNvimOnlyWhenItCanBeReached(t *testing.T) {
	with, _, _ := terminalModel(t, 150, 40)
	with = drive(with, key("r"))
	if !strings.Contains(screen(with), "o nvim") {
		t.Errorf("footer lacks o:\n%s", screen(with))
	}
	without, _ := reviewModel(t, 150, 40)
	without = drive(without, key("r"))
	if strings.Contains(screen(without), "o nvim") {
		t.Errorf("footer offers o without a caller:\n%s", screen(without))
	}
}

func TestReviewShowsTheSessionsDraftCommentCount(t *testing.T) {
	m, _, _ := terminalModel(t, 150, 40)
	m = drive(m, key("r"))
	if strings.Contains(screen(m), "draft") {
		t.Fatalf("no comments yet, but the review mentions a draft:\n%s", screen(m))
	}
	draft := func(id, session string, n int) tui.DiffMsg {
		return tui.DiffMsg(rpc.Diff{Seq: 99, Draft: &domain.ReviewDraft{ID: id, Session: session, Status: domain.DraftOpen, Comments: make([]domain.ReviewComment, n)}})
	}
	m = update(m, draft("d1", "s01", 1))
	m = update(m, draft("d2", "s02", 3))
	m = update(m, draft("d1", "s01", 2))
	if out := screen(m); !strings.Contains(out, "2 draft comments") {
		t.Fatalf("want the session's 2 comments, not the other's:\n%s", out)
	}
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 100, Draft: &domain.ReviewDraft{ID: "d1", Session: "s01", Status: domain.DraftSent, Comments: make([]domain.ReviewComment, 2)}}))
	if out := screen(m); strings.Contains(out, "draft comment") {
		t.Fatalf("a sent draft still counts:\n%s", out)
	}
}

func TestReviewCountsCommentsFromTheSnapshot(t *testing.T) {
	st, rv := reviewFixture()
	st.Drafts = []domain.ReviewDraft{{ID: "d1", Session: "s01", Status: domain.DraftOpen, Comments: []domain.ReviewComment{{Body: "x"}}}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Review: rv})
	m = update(m, tea.WindowSizeMsg{Width: 150, Height: 40})
	m = update(m, tui.StateMsg(st))
	m = drive(m, key("r"))
	if out := screen(m); !strings.Contains(out, "1 draft comment") || strings.Contains(out, "1 draft comments") {
		t.Fatalf("snapshot comments are not shown:\n%s", out)
	}
}
