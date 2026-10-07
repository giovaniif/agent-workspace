package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type sendEnv struct {
	reviewEnv
	host  *fakeHost
	hunks *fakeHunkGit
	c     *rpc.Client
}

func startSend(t *testing.T, state domain.AgentState, drafts ...domain.ReviewDraft) sendEnv {
	t.Helper()
	env := sendEnv{reviewEnv: reviewEnv{store: &memStore{}, git: newFakeReviewGit(), lister: &fakeLister{listings: map[string]domain.RepoListing{}}}, host: &fakeHost{panes: []app.PaneInfo{{ID: "%1", Alive: true}}}, hunks: &fakeHunkGit{}}
	env.store.snap.Workspaces = []domain.Workspace{{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo", DefaultBranch: "main"}}}}
	env.store.snap.Sessions = []domain.Session{{ID: "s1", Harness: domain.HarnessClaude, Pane: "%1", State: state, WorktreeIDs: []string{"/solo-feat"}}}
	env.store.snap.Worktrees = []domain.Worktree{{ID: "/solo-feat", Repo: "/solo", Path: "/solo-feat", Branch: "feat", SessionID: "s1"}}
	env.store.snap.Drafts = drafts
	env.git.trees["/solo-feat"] = "t1"
	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"})
	_, env.path = start(t, env.store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, &fakeFinder{prs: map[string][]domain.PullRequest{}}),
		daemon.WithWorktreePoll(time.Hour, time.Hour),
		daemon.WithHarnesses(env.host, claude.Adapter{}),
		daemon.WithReview(env.git),
		daemon.WithHunks(env.hunks),
	)
	env.c = dial(t, env.path)
	return env
}

var (
	commentA = domain.ReviewComment{Worktree: "/solo-feat", Path: "a.go", Start: 1, End: 1, Code: []string{"y"}, Body: "rename y"}
	commentB = domain.ReviewComment{Worktree: "/solo-feat", Path: "b.go", Start: 4, End: 6, Code: []string{"p", "q", "r"}, Body: "split this"}
)

func addComments(t *testing.T, c *rpc.Client, cs ...domain.ReviewComment) domain.ReviewDraft {
	t.Helper()
	var d domain.ReviewDraft
	var err error
	for _, x := range cs {
		if d, err = c.AddReviewComment(context.Background(), params(x)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func params(c domain.ReviewComment) rpc.CommentParams {
	return rpc.CommentParams{Session: "s1", Worktree: c.Worktree, Path: c.Path, StartLine: c.Start, EndLine: c.End,
		Code: strings.Join(c.Code, "\n"), Body: c.Body, Removed: c.Removed}
}

func withoutIDs(cs []domain.ReviewComment) []domain.ReviewComment {
	out := make([]domain.ReviewComment, len(cs))
	for i, c := range cs {
		c.ID = ""
		out[i] = c
	}
	return out
}

func hook(t *testing.T, c *rpc.Client, event string) {
	t.Helper()
	h := rpc.Hook{Harness: "claude", Event: event, Pane: "%1", At: time.Now(), Payload: []byte(`{"cwd":"/solo-feat"}`)}
	if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestReviewSendPastesTheDraftAsOnePromptAndArchivesIt(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	d := addComments(t, env.c, commentA, commentB)
	if d.Status != domain.DraftOpen || len(d.Comments) != 2 {
		t.Fatalf("draft %+v", d)
	}
	sent, err := env.c.SendReview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if sent.Status != domain.DraftSent || sent.ID != d.ID {
		t.Fatalf("sent %+v", sent)
	}
	want := []string{"%1 paste=true " + domain.ReviewPrompt([]domain.ReviewComment{commentA, commentB}), "%1 keys Enter"}
	if typed := env.host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
	rv, err := env.c.Review(context.Background(), rpc.ReviewParams{Session: "s1", Scope: domain.ScopeUncommitted})
	if err != nil || len(rv.Draft.Comments) != 0 {
		t.Errorf("after sending, the review's draft = %+v, %v; want a fresh one", rv.Draft, err)
	}
	if next := addComments(t, env.c, commentA); next.ID == d.ID {
		t.Errorf("a comment after sending joined the archived draft %q", next.ID)
	}
	if st := env.store.drafts(); len(st) < 1 || st[0].ID != d.ID || st[0].Status != domain.DraftSent {
		t.Errorf("stored drafts %+v", st)
	}
}

func TestReviewSendWithANoteAppendsItToTheSamePrompt(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	addComments(t, env.c, commentA)
	var sent domain.ReviewDraft
	if err := env.c.Call(context.Background(), rpc.MethodReviewSend, rpc.ReviewSendParams{Session: "s1", Note: "Also add tests."}, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Note != "Also add tests." {
		t.Errorf("sent note %q", sent.Note)
	}
	want := []string{"%1 paste=true " + domain.ReviewPrompt([]domain.ReviewComment{commentA}) + "\nOverall:\n   Also add tests.\n", "%1 keys Enter"}
	if typed := env.host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestReviewSendWhileRunningQueuesUntilTheNextStop(t *testing.T) {
	env := startSend(t, domain.StateRunning)
	addComments(t, env.c, commentA)
	queued, err := env.c.SendReview(context.Background(), "s1")
	if err != nil || queued.Status != domain.DraftQueued {
		t.Fatalf("queued %+v, %v", queued, err)
	}
	time.Sleep(50 * time.Millisecond)
	if typed := env.host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed while running: %q", typed)
	}
	hook(t, env.c, "Stop")
	want := []string{"%1 paste=true " + domain.ReviewPrompt([]domain.ReviewComment{commentA}), "%1 keys Enter"}
	if typed := env.host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
}

func TestReviewSentDraftLinksTheTurnItsPromptProduced(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	addComments(t, env.c, commentA)
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	env.host.waitTyped(t, 2)
	hook(t, env.c, "UserPromptSubmit")
	ref := domain.TurnRef("s1", "/solo-feat", 1)
	waitUntil(t, "draft linked", func() bool {
		st := env.store.drafts()
		return len(st) == 1 && reflect.DeepEqual(st[0].Turns, []string{ref})
	})
}

func TestReviewDraftsSurviveADaemonRestart(t *testing.T) {
	kept := domain.ReviewDraft{ID: "s1-9", Session: "s1", Status: domain.DraftQueued, Comments: []domain.ReviewComment{commentA}}
	env := startSend(t, domain.StateRunning, kept, domain.ReviewDraft{ID: "s1-1", Session: "s1", Status: domain.DraftSent, Comments: []domain.ReviewComment{commentB}})
	rv, err := env.c.Review(context.Background(), rpc.ReviewParams{Session: "s1", Scope: domain.ScopeUncommitted})
	if err != nil || !reflect.DeepEqual(rv.Draft, kept) {
		t.Fatalf("draft %+v, %v; want %+v", rv.Draft, err, kept)
	}
	hook(t, env.c, "Stop")
	if typed := env.host.waitTyped(t, 2); !strings.Contains(typed[0], "rename y") {
		t.Fatalf("the restored queued draft was not sent: %q", typed)
	}
}

func TestReviewSendRefusesAnEmptyDraftAndAnUnknownSession(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	var rerr *rpc.Error
	if _, err := env.c.SendReview(context.Background(), "s1"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("empty draft: %v", err)
	}
	unknown := params(commentA)
	unknown.Session = "nope"
	if _, err := env.c.AddReviewComment(context.Background(), unknown); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("unknown session: %v", err)
	}
	if _, err := env.c.AddReviewComment(context.Background(), params(domain.ReviewComment{Worktree: "/elsewhere", Path: "a.go", Start: 1, Body: "x"})); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("a worktree the session does not own: %v", err)
	}
}

func TestReviewHunkRunsInTheSessionsWorktreeOnly(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	f := domain.ParseDiff(reviewDiff)[0]
	ctx := context.Background()
	for _, a := range []domain.HunkAction{domain.HunkStage, domain.HunkRevert} {
		if err := env.c.ApplyHunk(ctx, rpc.HunkParams{Session: "s1", Worktree: "/solo-feat", File: f, Hunk: 0, Action: a}); err != nil {
			t.Fatal(err)
		}
	}
	if got := env.hunks.done(); !reflect.DeepEqual(got, []string{"stage /solo-feat", "revert /solo-feat"}) {
		t.Errorf("hunk calls %q", got)
	}
	var rerr *rpc.Error
	for _, p := range []rpc.HunkParams{
		{Session: "s1", Worktree: "/elsewhere", File: f, Action: domain.HunkStage},
		{Session: "s1", Worktree: "/solo-feat", File: f, Hunk: 5, Action: domain.HunkStage},
	} {
		if err := env.c.ApplyHunk(ctx, p); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
			t.Errorf("%+v: %v", p, err)
		}
	}
	if len(env.hunks.done()) != 2 {
		t.Errorf("a refused hunk reached git: %q", env.hunks.done())
	}
}

func TestReviewSendStoresADraftAsSentOnlyOnceItIsPasted(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	addComments(t, env.c, commentA)
	release := env.host.holdPane("%1")
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if st := env.store.drafts(); len(st) != 1 || st[0].Status != domain.DraftQueued {
		t.Fatalf("before the paste the store holds %+v; a restart now must resend it", st)
	}
	release()
	waitUntil(t, "stored as sent", func() bool {
		st := env.store.drafts()
		return len(st) == 1 && st[0].Status == domain.DraftSent
	})
}

func TestReviewSendWaitsForTheDraftBeingPastedBeforeTheNext(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	addComments(t, env.c, commentA)
	release := env.host.holdPane("%1")
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	addComments(t, env.c, commentB)
	second, err := env.c.SendReview(context.Background(), "s1")
	if err != nil || second.Status != domain.DraftQueued {
		t.Fatalf("a send during another paste = %+v, %v; want it queued", second, err)
	}
	release()
	want := []string{
		"%1 paste=true " + domain.ReviewPrompt([]domain.ReviewComment{commentA}), "%1 keys Enter",
		"%1 paste=true " + domain.ReviewPrompt([]domain.ReviewComment{commentB}), "%1 keys Enter",
	}
	if typed := env.host.waitTyped(t, len(want)); !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %q", typed)
	}
	waitUntil(t, "both stored as sent", func() bool {
		st := env.store.drafts()
		return len(st) == 2 && st[0].Status == domain.DraftSent && st[1].Status == domain.DraftSent
	})
}

func TestReviewSendAFailedPasteGoesBackToTheQueueWithCommentsAddedSince(t *testing.T) {
	env := startSend(t, domain.StateIdle)
	addComments(t, env.c, commentA)
	env.host.failOn(domain.ReviewPrompt([]domain.ReviewComment{commentA}))
	release := env.host.holdPane("%1")
	if _, err := env.c.SendReview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	addComments(t, env.c, commentB)
	release()
	var rv rpc.Review
	waitUntil(t, "draft requeued", func() bool {
		rv, _ = env.c.Review(context.Background(), rpc.ReviewParams{Session: "s1", Scope: domain.ScopeUncommitted})
		return rv.Draft.Status == domain.DraftQueued
	})
	if want := []domain.ReviewComment{commentA, commentB}; !reflect.DeepEqual(withoutIDs(rv.Draft.Comments), want) {
		t.Errorf("requeued comments %+v, want %+v", rv.Draft.Comments, want)
	}
	live := 0
	for _, d := range env.store.drafts() {
		if d.Status == domain.DraftOpen || d.Status == domain.DraftQueued {
			live++
		}
	}
	if live != 1 {
		t.Errorf("stored %d live drafts, want 1: %+v", live, env.store.drafts())
	}
}
