package tui_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type fakeReviewer struct {
	reply   rpc.Review
	err     error
	asked   []rpc.ReviewParams
	marked  []viewedCall
	layouts []bool

	comments   []rpc.CommentParams
	draft      domain.ReviewDraft
	sent       []string
	sendStatus domain.DraftStatus
	hunks      []rpc.HunkParams
}

type viewedCall struct {
	mark   domain.ViewedMark
	viewed bool
}

func (f *fakeReviewer) Review(_ context.Context, p rpc.ReviewParams) (rpc.Review, error) {
	f.asked = append(f.asked, p)
	if f.err != nil {
		return rpc.Review{}, f.err
	}
	r := f.reply
	r.Scope = p.Scope
	return r, nil
}

func (f *fakeReviewer) MarkViewed(_ context.Context, m domain.ViewedMark, viewed bool) error {
	f.marked = append(f.marked, viewedCall{m, viewed})
	return nil
}

func (f *fakeReviewer) AddReviewComment(_ context.Context, p rpc.CommentParams) (domain.ReviewDraft, error) {
	f.comments = append(f.comments, p)
	f.draft = f.draft.Add(domain.ReviewComment{Path: p.Path, Start: p.StartLine, End: p.EndLine, Body: p.Body})
	f.draft.Session = p.Session
	return f.draft, nil
}

func (f *fakeReviewer) SendReview(_ context.Context, session string) (domain.ReviewDraft, error) {
	f.sent = append(f.sent, session)
	f.draft.Status = f.sendStatus
	return f.draft, nil
}

func (f *fakeReviewer) ApplyHunk(_ context.Context, p rpc.HunkParams) error {
	f.hunks = append(f.hunks, p)
	return nil
}

func (f *fakeReviewer) ReviewLayout(_ context.Context, open bool) error {
	f.layouts = append(f.layouts, open)
	return nil
}

type call struct {
	method string
	params any
}

type fakeCaller struct {
	mu     sync.Mutex
	calls  []call
	err    error
	failOn string
	dirs   map[string][]domain.Child
	autos  []domain.Automation
	asks   []domain.Approval
}

func (f *fakeCaller) Call(_ context.Context, method string, params, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{method, params})
	if f.err != nil && (f.failOn == "" || f.failOn == method) {
		return f.err
	}
	if method == rpc.MethodWorkspaceDirs {
		children, ok := f.dirs[params.(rpc.WorkspaceDirsParams).Path]
		if !ok {
			return &rpc.Error{Code: rpc.CodeNotFound, Message: "no such directory"}
		}
		*out.(*rpc.WorkspaceDirs) = rpc.WorkspaceDirs{Dirs: children}
		return nil
	}
	if method == rpc.MethodApprovalList {
		*out.(*[]domain.Approval) = f.asks
		return nil
	}
	if method == rpc.MethodAutomationList {
		*out.(*rpc.AutomationList) = rpc.AutomationList{Automations: f.autos}
		return nil
	}
	if method == rpc.MethodNewSession {
		if s, ok := out.(*domain.Session); ok {
			b, _ := json.Marshal(domain.Session{ID: "new1", Pane: "%5", State: domain.StateIdle})
			return json.Unmarshal(b, s)
		}
	}
	return nil
}

type fakeAttender struct {
	muted   []mute
	focused []string
}

type mute struct {
	id    string
	muted bool
}

func (f *fakeAttender) MuteSession(_ context.Context, id string, muted bool) error {
	f.muted = append(f.muted, mute{id, muted})
	return nil
}

func (f *fakeAttender) FocusSession(_ context.Context, id string) error {
	f.focused = append(f.focused, id)
	return nil
}

type fakeKiller struct {
	killed [][]int
	err    error
}

func (f *fakeKiller) KillPorts(_ context.Context, pgids []int) ([]int, error) {
	f.killed = append(f.killed, pgids)
	return pgids, f.err
}

type fakeFocuser struct{ calls int }

func (f *fakeFocuser) FocusMain(context.Context) error {
	f.calls++
	return nil
}

func (f *fakeCaller) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

type fakeSwitcher struct {
	calls []string
	err   error
}

func (f *fakeSwitcher) SwitchSession(_ context.Context, id string, kind domain.SwitchKind, value string) (domain.Session, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s %s %s", id, kind, value))
	return domain.Session{ID: id}, f.err
}

type fakeDisker struct {
	mu      sync.Mutex
	views   []rpc.DiskView
	fetches int
	actions []diskAction
	shells  []string
	layouts []bool
	item    rpc.CleanupItem
	err     error
}

type diskAction struct {
	path   string
	backup bool
}

func (f *fakeDisker) DiskView(context.Context) (rpc.DiskView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := min(f.fetches, len(f.views)-1)
	f.fetches++
	return f.views[i], f.err
}

func (f *fakeDisker) CleanupWorktree(_ context.Context, path string, backup bool) (rpc.CleanupItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, diskAction{path, backup})
	return f.item, f.err
}

func (f *fakeDisker) WorktreeShell(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shells = append(f.shells, id)
	return nil
}

func (f *fakeDisker) ReviewLayout(_ context.Context, open bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.layouts = append(f.layouts, open)
	return nil
}

func (f *fakeDisker) fetched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}
