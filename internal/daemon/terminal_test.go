package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type termRig struct {
	c      *rpc.Client
	path   string
	home   string
	term   *termFake
	client *fakeClientHost
	editor *fakeEditor
	slot   app.Slot
}

func startTerm(t *testing.T, sessions []domain.Session, worktrees []domain.Worktree) termRig {
	t.Helper()
	store := &memStore{}
	store.snap.Sessions = sessions
	store.snap.Worktrees = worktrees
	r := termRig{client: &fakeClientHost{}, editor: &fakeEditor{}, home: shortDir(t)}
	live := []app.PaneID{}
	for _, s := range sessions {
		live = append(live, app.PaneID(s.Pane))
	}
	r.term = newTermFake(r.client, live...)
	d, path := start(t, store,
		daemon.WithHarnesses(r.term, claude.Adapter{}),
		daemon.WithTerminals(r.home, r.editor))
	d.SetClientHost(r.client)
	r.path = path
	r.c = dial(t, path)
	opened, err := r.c.OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"agentws", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	r.slot = app.Slot(opened.Slot)
	return r
}

var (
	termSession = domain.Session{ID: "s1", Pane: "%a1", Harness: domain.HarnessClaude}
	termWTs     = []domain.Worktree{
		{ID: "w-api", Path: "/wt/api", SessionID: "s1"},
		{ID: "w-web", Path: "/wt/web", SessionID: "s1"},
	}
)

func (r termRig) toggle(t *testing.T, p rpc.ShellParams) (rpc.ShellResult, error) {
	t.Helper()
	var out rpc.ShellResult
	err := r.c.Call(context.Background(), rpc.MethodShellToggle, p, &out)
	return out, err
}

func TestShellToggleKeepsOneShellPerSessionAndWorktree(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)

	first, err := r.toggle(t, rpc.ShellParams{Session: "s1", Worktree: "w-web"})
	if err != nil {
		t.Fatal(err)
	}
	specs := r.term.createdSpecs()
	if len(specs) != 1 || specs[0].Dir != "/wt/web" || len(specs[0].Command) != 0 || !strings.HasPrefix(specs[0].Name, "shell-") {
		t.Fatalf("shell spec = %+v; want a default shell in /wt/web", specs)
	}
	if specs[0].Env["AGENTWS_SESSION"] != "s1" || specs[0].Env["AGENTWS_HOME"] != r.home {
		t.Fatalf("shell env = %v; want the session and home", specs[0].Env)
	}
	if !first.Shown || first.Pane != "%t1" || first.Dir != "/wt/web" {
		t.Fatalf("first toggle = %+v; want the shell shown", first)
	}
	if got := r.term.shownPanes(); !slices.Equal(got, []app.PaneID{"%a1"}) {
		t.Fatalf("the agent pane was shown as %v; want it in the slot first", got)
	}
	if got := r.client.belowCalls(); !slices.Equal(got, []string{"show %t1"}) {
		t.Fatalf("below calls = %v", got)
	}

	hidden, err := r.toggle(t, rpc.ShellParams{Session: "s1", Worktree: "w-web"})
	if err != nil || hidden.Shown {
		t.Fatalf("second toggle = %+v, %v; want it hidden", hidden, err)
	}
	again, err := r.toggle(t, rpc.ShellParams{Session: "s1", Worktree: "w-web"})
	if err != nil || !again.Shown || again.Pane != first.Pane || len(r.term.createdSpecs()) != 1 {
		t.Fatalf("third toggle = %+v, %v with %d shells created; want the first shell back", again, err, len(r.term.createdSpecs()))
	}

	other, err := r.toggle(t, rpc.ShellParams{Session: "s1", Worktree: "w-api"})
	if err != nil || other.Pane == first.Pane || other.Dir != "/wt/api" {
		t.Fatalf("other worktree = %+v, %v; want its own shell in /wt/api", other, err)
	}
}

func TestShellToggleNeverMovesFocusToTheAgentPane(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	for range 2 {
		if _, err := r.toggle(t, rpc.ShellParams{Session: "s1"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.client.focused) != 0 {
		t.Fatalf("focus moved to the slot %v times; showing the agent must leave focus with the caller or the shell", len(r.client.focused))
	}
}

func TestShellToggleKeepsNvimInTheSlotWhenItIsThere(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	if err := r.c.Call(context.Background(), rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.toggle(t, rpc.ShellParams{Session: "s1"}); err != nil {
		t.Fatal(err)
	}
	if got := r.term.shownPanes(); !slices.Equal(got, []app.PaneID{"%t1"}) {
		t.Fatalf("shown = %v; want the shell split under nvim, with the agent left parked", got)
	}
}

func TestShellToggleStartsAFreshShellWhenTheOldOneExited(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	first, _ := r.toggle(t, rpc.ShellParams{Session: "s1"})
	r.term.kill(app.PaneID(first.Pane))
	second, err := r.toggle(t, rpc.ShellParams{Session: "s1"})
	if err != nil || !second.Shown || second.Pane == first.Pane {
		t.Fatalf("toggle after exit = %+v, %v; want a new shell shown", second, err)
	}
	if got := r.client.belowCalls(); slices.Contains(got, "hide "+first.Pane) {
		t.Fatalf("below calls = %v; the exited shell should not be hidden as if it were open", got)
	}
}

func TestShellToggleFallsBackToTheSessionDirectory(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, nil)
	if err := r.c.Call(context.Background(), rpc.MethodHook, rpc.Hook{Harness: "claude", Event: "SessionStart", Pane: "%a1", Payload: []byte(`{"cwd":"/src/shop"}`)}, nil); err != nil {
		t.Fatal(err)
	}
	out, err := r.toggle(t, rpc.ShellParams{Session: "s1"})
	if err != nil || out.Dir != "/src/shop" {
		t.Fatalf("toggle = %+v, %v; want a shell in the hook's cwd", out, err)
	}
}

func TestShellToggleRefusesWhatItCannotPlace(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	cases := []struct {
		name string
		p    rpc.ShellParams
		code string
	}{
		{"unknown session", rpc.ShellParams{Session: "nope"}, rpc.CodeNotFound},
		{"a worktree the session does not own", rpc.ShellParams{Session: "s1", Worktree: "w-x"}, rpc.CodeBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.toggle(t, c.p)
			var rerr *rpc.Error
			if !errors.As(err, &rerr) || rerr.Code != c.code {
				t.Fatalf("err = %v; want code %s", err, c.code)
			}
		})
	}
	if len(r.term.createdSpecs()) != 0 {
		t.Fatalf("panes created for refused requests: %+v", r.term.createdSpecs())
	}
}

func TestShellToggleByWorktreeAloneUsesItsOwnerOrNone(t *testing.T) {
	wts := append([]domain.Worktree{{ID: "w-free", Path: "/wt/free"}}, termWTs...)
	r := startTerm(t, []domain.Session{termSession}, wts)
	owned, err := r.toggle(t, rpc.ShellParams{Worktree: "w-web"})
	if err != nil || owned.Dir != "/wt/web" {
		t.Fatalf("owned = %+v, %v; want a shell in /wt/web", owned, err)
	}
	same, err := r.toggle(t, rpc.ShellParams{Session: "s1", Worktree: "w-web"})
	if err != nil || same.Shown || same.Pane != owned.Pane {
		t.Fatalf("by session = %+v, %v; want the same shell, now hidden", same, err)
	}
	free, err := r.toggle(t, rpc.ShellParams{Worktree: "w-free"})
	if err != nil || free.Dir != "/wt/free" || free.Pane == owned.Pane {
		t.Fatalf("unowned = %+v, %v; want its own shell in /wt/free", free, err)
	}
	var rerr *rpc.Error
	if _, err := r.toggle(t, rpc.ShellParams{Worktree: "w-x"}); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("unknown worktree: %v; want not_found", err)
	}
	if _, err := r.toggle(t, rpc.ShellParams{}); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("no session or worktree: %v; want bad_request", err)
	}
}

func TestShellPopupOpensThePaneInAPopupNotASplit(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	out, err := r.toggle(t, rpc.ShellParams{Session: "s1", Popup: true})
	if err != nil || out.Pane != "%t1" {
		t.Fatalf("popup toggle = %+v, %v", out, err)
	}
	if !slices.Equal(r.client.popups, []app.PaneID{"%t1"}) || len(r.client.belowCalls()) != 0 {
		t.Fatalf("popups %v, below calls %v; want one popup and no split", r.client.popups, r.client.belowCalls())
	}
}

func TestNvimToggleStartsAListeningNvimAndSwapsItWithTheAgent(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	sock := filepath.Join(r.home, "nvim", "s1.sock")

	var out rpc.NvimResult
	if err := r.c.Call(context.Background(), rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}, &out); err != nil {
		t.Fatal(err)
	}
	specs := r.term.createdSpecs()
	want := app.PaneSpec{Name: "nvim-s1", Dir: "/wt/api", Command: []string{"nvim", "--listen", sock},
		Env: map[string]string{"AGENTWS_SESSION": "s1", "AGENTWS_HOME": r.home}}
	if len(specs) != 1 || !reflect.DeepEqual(specs[0], want) {
		t.Fatalf("nvim spec = %+v; want %+v", specs, want)
	}
	if out.Socket != sock || !out.Shown {
		t.Fatalf("result = %+v", out)
	}
	if got := r.term.shownPanes(); !slices.Equal(got, []app.PaneID{"%t1"}) {
		t.Fatalf("shown = %v; want nvim in the slot", got)
	}

	if err := r.c.Call(context.Background(), rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := r.term.shownPanes(); !slices.Equal(got, []app.PaneID{"%t1", "%a1"}) || out.Shown {
		t.Fatalf("shown = %v, result %+v; want the agent back in the slot", got, out)
	}
	if err := r.c.Call(context.Background(), rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}, &out); err != nil || len(r.term.createdSpecs()) != 1 || !out.Shown {
		t.Fatalf("third toggle: %v, %d panes created, %+v; want the same nvim shown again", err, len(r.term.createdSpecs()), out)
	}
}

func TestNvimOpenStartsNvimOnTheFileOrTellsARunningOne(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	sock := filepath.Join(r.home, "nvim", "s1.sock")

	if err := r.c.Call(context.Background(), rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-web", Path: "src/app.ts", Line: 14}, nil); err != nil {
		t.Fatal(err)
	}
	specs := r.term.createdSpecs()
	if len(specs) != 1 || !slices.Equal(specs[0].Command, []string{"nvim", "--listen", sock, "+14", "/wt/web/src/app.ts"}) || specs[0].Dir != "/wt/web" {
		t.Fatalf("nvim spec = %+v; want nvim started on the file", specs)
	}
	if len(r.editor.evals()) != 0 {
		t.Fatalf("editor called for a fresh nvim: %v", r.editor.evals())
	}

	if err := r.c.Call(context.Background(), rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-api", Path: "it's.go", Line: 3}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{sock + " " + domain.NvimOpenExpr("/wt/api/it's.go", 3)}
	if got := r.editor.evals(); !slices.Equal(got, want) || len(r.term.createdSpecs()) != 1 {
		t.Fatalf("editor calls = %v, %d panes; want the running nvim told to open the file", got, len(r.term.createdSpecs()))
	}
	if got := r.term.shownPanes(); len(got) == 0 || got[len(got)-1] != "%t1" {
		t.Fatalf("shown = %v; want nvim in the slot", got)
	}
}

func TestNvimOpenRefusesAPathOutsideTheWorktree(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	err := r.c.Call(context.Background(), rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-api", Path: "../web/secret", Line: 1}, nil)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest || len(r.term.createdSpecs()) != 0 {
		t.Fatalf("err = %v, panes %+v; want bad_request and no nvim", err, r.term.createdSpecs())
	}
}

func commentRig(t *testing.T) termRig {
	return startTerm(t, []domain.Session{termSession}, termWTs)
}

func TestReviewCommentFromAFileBecomesADraftEveryoneSees(t *testing.T) {
	r := commentRig(t)
	sub, err := dial(t, r.path).Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var draft domain.ReviewDraft
	err = r.c.Call(context.Background(), rpc.MethodReviewComment, rpc.CommentParams{
		Session: "s1", File: "/wt/web/src/app.ts", StartLine: 9, EndLine: 4, Code: "let x = 1", Body: "why not const?",
	}, &draft)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Session != "s1" || draft.Status != domain.DraftOpen || len(draft.Comments) != 1 {
		t.Fatalf("draft = %+v", draft)
	}
	got := draft.Comments[0]
	if got.ID == "" || got.Worktree != "/wt/web" || got.Path != "src/app.ts" || got.Start != 4 || got.End != 9 || got.Body != "why not const?" || !slices.Equal(got.Code, []string{"let x = 1"}) {
		t.Fatalf("comment = %+v", got)
	}
	select {
	case d := <-sub.Diffs:
		if d.Comment == nil || d.Comment.ID != got.ID || d.Draft == nil || d.Draft.ID != draft.ID {
			t.Fatalf("diff = %+v; want the comment and its draft", d)
		}
	case <-time.After(time.Second):
		t.Fatal("the comment did not reach a subscriber within 1 s")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("comment took %v", elapsed)
	}
	late, err := dial(t, r.path).Subscribe(context.Background())
	if err != nil || len(late.State.Drafts) != 1 || late.State.Drafts[0].Comments[0].ID != got.ID {
		t.Fatalf("a later subscriber's drafts = %+v, %v", late.State.Drafts, err)
	}
}

func TestReviewCommentByWorktreeAndPath(t *testing.T) {
	r := commentRig(t)
	var draft domain.ReviewDraft
	err := r.c.Call(context.Background(), rpc.MethodReviewComment, rpc.CommentParams{
		Session: "s1", Worktree: "w-api", Path: "main.go", StartLine: 2, Body: "rename",
	}, &draft)
	if err != nil || len(draft.Comments) != 1 {
		t.Fatalf("draft = %+v, %v", draft, err)
	}
	if got := draft.Comments[0]; got.Worktree != "/wt/api" || got.Path != "main.go" || got.Start != 2 || got.End != 2 {
		t.Fatalf("comment = %+v", got)
	}
}

func TestReviewCommentRefusesWhatItCannotPlace(t *testing.T) {
	r := commentRig(t)
	cases := []struct {
		name string
		p    rpc.CommentParams
		code string
	}{
		{"unknown session", rpc.CommentParams{Session: "nope", File: "/wt/api/a.go", StartLine: 1, Body: "x"}, rpc.CodeNotFound},
		{"a file in no worktree of the session", rpc.CommentParams{Session: "s1", File: "/elsewhere/a.go", StartLine: 1, Body: "x"}, rpc.CodeBadRequest},
		{"no line", rpc.CommentParams{Session: "s1", File: "/wt/api/a.go", Body: "x"}, rpc.CodeBadRequest},
		{"an empty comment", rpc.CommentParams{Session: "s1", File: "/wt/api/a.go", StartLine: 1, Body: "  "}, rpc.CodeBadRequest},
		{"an unknown worktree", rpc.CommentParams{Session: "s1", Worktree: "w-x", Path: "a.go", StartLine: 1, Body: "x"}, rpc.CodeBadRequest},
		{"a path that climbs out of the worktree", rpc.CommentParams{Session: "s1", Worktree: "w-api", Path: "../web/a.go", StartLine: 1, Body: "x"}, rpc.CodeBadRequest},
		{"an absolute path with a worktree", rpc.CommentParams{Session: "s1", Worktree: "w-api", Path: "/wt/api/a.go", StartLine: 1, Body: "x"}, rpc.CodeBadRequest},
		{"the worktree directory itself", rpc.CommentParams{Session: "s1", Worktree: "w-api", Path: ".", StartLine: 1, Body: "x"}, rpc.CodeBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := r.c.Call(context.Background(), rpc.MethodReviewComment, c.p, nil)
			var rerr *rpc.Error
			if !errors.As(err, &rerr) || rerr.Code != c.code {
				t.Fatalf("err = %v; want code %s", err, c.code)
			}
		})
	}
	late, _ := dial(t, r.path).Subscribe(context.Background())
	if len(late.State.Drafts) != 0 {
		t.Fatalf("refused comments were kept: %+v", late.State.Drafts)
	}
}

func TestShellFocusShowsTheShellAndPutsFocusInIt(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	var out rpc.ShellResult
	if err := r.c.Call(context.Background(), rpc.MethodShellFocus, rpc.ShellParams{Session: "s1"}, &out); err != nil {
		t.Fatal(err)
	}
	if r.client.below != app.PaneID(out.Pane) || r.client.focusedBelow != 1 {
		t.Fatalf("below %q, focused below %d times; want the shell shown and focused once", r.client.below, r.client.focusedBelow)
	}
	if err := r.c.Call(context.Background(), rpc.MethodShellFocus, rpc.ShellParams{Session: "s1"}, &out); err != nil {
		t.Fatal(err)
	}
	if r.client.below != app.PaneID(out.Pane) || r.client.focusedBelow != 2 {
		t.Fatalf("a second focus hid the shell: below %q, focused %d", r.client.below, r.client.focusedBelow)
	}
}

func TestTerminalsWithoutAClientLayoutStillHandBackTheirPanes(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{termSession}
	store.snap.Worktrees = termWTs
	term := newTermFake(&fakeClientHost{}, "%a1")
	_, path := start(t, store,
		daemon.WithHarnesses(term, claude.Adapter{}),
		daemon.WithTerminals(shortDir(t), &fakeEditor{}))
	c := dial(t, path)
	for _, method := range []string{rpc.MethodShellToggle, rpc.MethodShellFocus} {
		var out rpc.ShellResult
		if err := c.Call(context.Background(), method, rpc.ShellParams{Session: "s1", Worktree: "w-web"}, &out); err != nil || out.Pane == "" || out.Dir != "/wt/web" {
			t.Fatalf("%s = %+v, %v; want the shell pane with no layout open", method, out, err)
		}
	}
	for _, call := range []struct {
		method string
		params rpc.NvimParams
	}{
		{rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}},
		{rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Worktree: "w-web", Path: "main.go", Line: 3}},
	} {
		var out rpc.NvimResult
		if err := c.Call(context.Background(), call.method, call.params, &out); err != nil || out.Pane == "" {
			t.Fatalf("%s = %+v, %v; want the nvim pane with no layout open", call.method, out, err)
		}
	}
}
