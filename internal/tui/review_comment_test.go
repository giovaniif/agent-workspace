package tui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func asked(wt string, c domain.ReviewComment) rpc.CommentParams {
	return rpc.CommentParams{Session: "s01", Worktree: wt, Path: c.Path, StartLine: c.Start, EndLine: c.End,
		Code: strings.Join(c.Code, "\n"), Body: c.Body, Removed: c.Removed}
}

func resolversLines() []domain.DiffLine {
	return domain.ParseDiff(resolversDiff)[0].Hunks[0].Lines
}

func TestReviewCommentOnTheCursorLineGoesToTheDraft(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "j", "c")...)
	if out := screen(m); !strings.Contains(out, "comment:") {
		t.Fatalf("c opens the comment input:\n%s", out)
	}
	m = drive(m, key("why"), key("space"), key("?"), tea.KeyPressMsg{Code: tea.KeyBackspace}, key("!"), key("enter"))
	st, _ := reviewFixture()
	want := []rpc.CommentParams{asked(st.Worktrees[0].ID, domain.CommentOn(st.Worktrees[0].Path, "src/graphql/public/resolvers.ts", resolversLines()[1:2], "why !"))}
	if !reflect.DeepEqual(rv.comments, want) {
		t.Fatalf("comments %+v\nwant %+v", rv.comments, want)
	}
	if out := screen(m); !strings.Contains(out, "1 draft comment") || strings.Contains(out, "comment:") {
		t.Errorf("the draft count shows and the input closes:\n%s", out)
	}
}

func TestReviewRangeCommentCoversEveryLineFromTheMark(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	drive(m, keys("r", "j", "j", "j", "V", "j", "j", "c", "x", "enter")...)
	st, _ := reviewFixture()
	want := asked(st.Worktrees[0].ID, domain.CommentOn(st.Worktrees[0].Path, "src/graphql/public/resolvers.ts", resolversLines()[3:6], "x"))
	if len(rv.comments) != 1 || !reflect.DeepEqual(rv.comments[0], want) {
		t.Fatalf("comments %+v\nwant %+v", rv.comments, want)
	}
}

func TestReviewRangeCommentInSplitViewTakesBothSides(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	drive(m, keys("r", "u", "j", "j", "j", "V", "j", "c", "x", "enter")...)
	st, _ := reviewFixture()
	want := asked(st.Worktrees[0].ID, domain.CommentOn(st.Worktrees[0].Path, "src/graphql/public/resolvers.ts", resolversLines()[3:7], "x"))
	if len(rv.comments) != 1 || !reflect.DeepEqual(rv.comments[0], want) {
		t.Fatalf("comments %+v\nwant %+v", rv.comments, want)
	}
}

func TestReviewEscCancelsTheCommentNotTheReview(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "c", "abc")...)
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(rv.comments) != 0 || !strings.Contains(screen(m), "REVIEW") || strings.Contains(screen(m), "comment:") {
		t.Errorf("esc should drop the comment and stay in the review; comments %+v:\n%s", rv.comments, screen(m))
	}
	drive(m, key("c"), key("enter"))
	if len(rv.comments) != 0 {
		t.Errorf("an empty comment was sent: %+v", rv.comments)
	}
}

func TestReviewSendShowsWhetherTheDraftWasSentOrQueued(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	rv.sendStatus = domain.DraftQueued
	m = drive(m, keys("r", "c", "a", "enter", "S")...)
	if !reflect.DeepEqual(rv.sent, []string{"s01"}) {
		t.Fatalf("sent %v", rv.sent)
	}
	if out := screen(m); !strings.Contains(out, "queued") {
		t.Errorf("a queued send says so:\n%s", out)
	}
	rv.sendStatus = domain.DraftSent
	m = drive(m, key("S"))
	if out := screen(m); !strings.Contains(out, "sent 1 comment") || strings.Count(out, "1 comment") != 1 {
		t.Errorf("a sent draft empties the count:\n%s", out)
	}
}

func TestReviewSendWithAnEmptyDraftAsksNothing(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	drive(m, keys("r", "S")...)
	if len(rv.sent) != 0 {
		t.Errorf("sent an empty draft: %v", rv.sent)
	}
}

func TestReviewStageAndRevertTheHunkUnderTheCursor(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	m = drive(m, keys("r", "j", "j", "s")...)
	file := rv.reply.Worktrees[0].Files[0]
	stage := rpc.HunkParams{Session: "s01", Worktree: "w01-0", File: file, Hunk: 0, Action: domain.HunkStage}
	if !reflect.DeepEqual(rv.hunks, []rpc.HunkParams{stage}) {
		t.Fatalf("hunks %+v", rv.hunks)
	}
	if len(rv.asked) != 2 {
		t.Errorf("staging refreshes the review; asked %d times", len(rv.asked))
	}
	m = drive(m, key("x"))
	if out := screen(m); !strings.Contains(out, "revert this hunk? y/n") || len(rv.hunks) != 1 {
		t.Fatalf("x asks first:\n%s", out)
	}
	m = drive(m, key("n"))
	if len(rv.hunks) != 1 || strings.Contains(screen(m), "y/n") {
		t.Fatalf("n cancels; hunks %+v", rv.hunks)
	}
	drive(m, key("x"), key("y"))
	revert := stage
	revert.Action = domain.HunkRevert
	if !reflect.DeepEqual(rv.hunks, []rpc.HunkParams{stage, revert}) {
		t.Errorf("hunks %+v", rv.hunks)
	}
}

func TestReviewDraftFromTheDaemonShowsItsCount(t *testing.T) {
	m, rv := reviewModel(t, 150, 40)
	rv.reply.Draft = domain.ReviewDraft{ID: "d", Status: domain.DraftOpen, Comments: []domain.ReviewComment{{Body: "a"}, {Body: "b"}}}
	if out := screen(drive(m, key("r"))); !strings.Contains(out, "2 draft comments") {
		t.Errorf("the restored draft's count shows:\n%s", out)
	}
}
