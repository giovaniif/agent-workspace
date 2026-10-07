package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type memStore struct {
	mu   sync.Mutex
	snap app.Snapshot
}

func (s *memStore) PutWorkspace(w domain.Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Workspaces = append(s.snap.Workspaces, w)
}

func (s *memStore) DeleteWorkspace(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.snap.Workspaces[:0]
	for _, w := range s.snap.Workspaces {
		if w.Root != root {
			kept = append(kept, w)
		}
	}
	s.snap.Workspaces = kept
}

func (s *memStore) PutTask(x domain.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Tasks = append(s.snap.Tasks, x)
}

func (s *memStore) PutWorktree(w domain.Worktree) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Worktrees = append(s.snap.Worktrees, w)
}

func (s *memStore) DeleteWorktree(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.snap.Worktrees[:0]
	for _, w := range s.snap.Worktrees {
		if w.ID != id {
			kept = append(kept, w)
		}
	}
	s.snap.Worktrees = kept
}

func (s *memStore) PutSession(x domain.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Sessions = append(s.snap.Sessions, x)
}

func (s *memStore) PutEvent(ev domain.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Events = append(s.snap.Events, ev)
}

func (s *memStore) PutViewed(m domain.ViewedMark) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteViewedLocked(m.Key())
	s.snap.Viewed = append(s.snap.Viewed, m)
}

func (s *memStore) DeleteViewed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteViewedLocked(key)
}

func (s *memStore) deleteViewedLocked(key string) {
	kept := s.snap.Viewed[:0]
	for _, m := range s.snap.Viewed {
		if m.Key() != key {
			kept = append(kept, m)
		}
	}
	s.snap.Viewed = kept
}

func (s *memStore) PutDraft(d domain.ReviewDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.snap.Drafts {
		if x.ID == d.ID {
			s.snap.Drafts[i] = d
			return
		}
	}
	s.snap.Drafts = append(s.snap.Drafts, d)
}

func (s *memStore) PutDevice(d domain.Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.snap.Devices {
		if x.ID == d.ID {
			s.snap.Devices[i] = d
			return
		}
	}
	s.snap.Devices = append(s.snap.Devices, d)
}

func (s *memStore) DeleteDevice(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.snap.Devices[:0]
	for _, d := range s.snap.Devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	s.snap.Devices = kept
}

func (s *memStore) PutProject(p domain.Project) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.snap.Projects {
		if x.Root == p.Root {
			s.snap.Projects[i] = p
			return
		}
	}
	s.snap.Projects = append(s.snap.Projects, p)
}

func (s *memStore) DeleteProject(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.snap.Projects[:0]
	for _, p := range s.snap.Projects {
		if p.Root != root {
			kept = append(kept, p)
		}
	}
	s.snap.Projects = kept
}

func (s *memStore) devices() []domain.Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Device(nil), s.snap.Devices...)
}

func (s *memStore) drafts() []domain.ReviewDraft {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ReviewDraft(nil), s.snap.Drafts...)
}

func (s *memStore) Load() (app.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap, nil
}

func (s *memStore) Flush() error { return nil }
func (s *memStore) Close() error { return nil }

type fakeFS struct {
	gate     chan struct{}
	markers  map[string]domain.GitMarker
	children map[string][]domain.Child
}

func (f fakeFS) Marker(path string) (domain.GitMarker, error) {
	if f.gate != nil {
		<-f.gate
	}
	m, ok := f.markers[path]
	if !ok {
		return domain.GitNone, errors.New("no such directory")
	}
	return m, nil
}

func (f fakeFS) Children(path string) ([]domain.Child, error) { return f.children[path], nil }

type fakeGit map[string]app.RepoFacts

func (g fakeGit) Inspect(_ context.Context, path string) (app.RepoFacts, error) {
	f, ok := g[path]
	if !ok {
		return app.RepoFacts{}, errors.New("not a repo")
	}
	return f, nil
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Minute)
	return c.now
}

type setClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *setClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *setClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type liveGit struct{ branch atomic.Value }

func (g *liveGit) Inspect(context.Context, string) (app.RepoFacts, error) {
	return app.RepoFacts{Branch: g.branch.Load().(string)}, nil
}

type fakeClientHost struct {
	mu          sync.Mutex
	opened      []app.PaneSpec
	open        map[app.Slot]bool
	focused     []app.Slot
	wide        []bool
	ensured     []app.Slot
	gone        bool
	duringCheck func()

	slotPane      map[app.Slot]app.PaneID
	below         app.PaneID
	belowLog      []string
	popups        []app.PaneID
	commandPopups []app.PaneSpec
	focusedBelow  int
	detached      []app.Slot

	native      app.NativeClient
	nativeSizes [][2]int
}

func (h *fakeClientHost) OpenNative(_ context.Context, cols, rows int) (app.NativeClient, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nativeSizes = append(h.nativeSizes, [2]int{cols, rows})
	return h.native, nil
}

func (h *fakeClientHost) SlotHasPane(context.Context, app.Slot) bool {
	h.mu.Lock()
	had := !h.gone
	h.gone = false
	during := h.duringCheck
	if !had {
		h.duringCheck = nil
	}
	h.mu.Unlock()
	if during != nil && !had {
		during()
	}
	return had
}

func (h *fakeClientHost) loseSlotPane() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gone = true
}

func (h *fakeClientHost) EnsureSlot(_ context.Context, slot app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensured = append(h.ensured, slot)
	return nil
}

func (h *fakeClientHost) OpenClient(_ context.Context, name string, tui app.PaneSpec) (app.Slot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opened = append(h.opened, tui)
	slot := app.Slot("@" + name + string(rune('0'+len(h.opened))))
	if h.open == nil {
		h.open = map[app.Slot]bool{}
	}
	h.open[slot] = true
	return slot, nil
}

func (h *fakeClientHost) ClientOpen(_ context.Context, slot app.Slot) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open[slot]
}

func (h *fakeClientHost) AttachCommand(slot app.Slot) []string {
	return []string{"tmux", "attach", "-t", string(slot)}
}

func (h *fakeClientHost) FocusSlot(_ context.Context, slot app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open[slot] {
		return errors.New("no such slot")
	}
	h.focused = append(h.focused, slot)
	return nil
}

func (h *fakeClientHost) WidenSidebar(_ context.Context, slot app.Slot, wide bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open[slot] {
		return errors.New("no such slot")
	}
	h.wide = append(h.wide, wide)
	return nil
}

func (h *fakeClientHost) setShownIn(slot app.Slot, pane app.PaneID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.slotPane == nil {
		h.slotPane = map[app.Slot]app.PaneID{}
	}
	h.slotPane[slot] = pane
}

func (h *fakeClientHost) ShownIn(_ context.Context, slot app.Slot) app.PaneID {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.slotPane[slot]
}

func (h *fakeClientHost) BelowPane(context.Context, app.Slot) app.PaneID {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.below
}

func (h *fakeClientHost) ShowBelow(_ context.Context, pane app.PaneID, _ app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.below = pane
	h.belowLog = append(h.belowLog, "show "+string(pane))
	return nil
}

func (h *fakeClientHost) HideBelow(context.Context, app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.belowLog = append(h.belowLog, "hide "+string(h.below))
	h.below = ""
	return nil
}

func (h *fakeClientHost) Popup(_ context.Context, pane app.PaneID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.popups = append(h.popups, pane)
	return nil
}

func (h *fakeClientHost) belowCalls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.belowLog...)
}

func (h *fakeClientHost) close(slot app.Slot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.open, slot)
}

type shown struct {
	pane app.PaneID
	slot app.Slot
}

type fakeHost struct {
	app.TerminalHost
	mu          sync.Mutex
	duringAlive func()
	titles      map[app.PaneID][]string
	specs       []app.PaneSpec
	err         error
	panes       []app.PaneInfo
	listed      int
	killed      []app.PaneID
	shown       []shown
	typed       []string
	failText    string
	gates       map[app.PaneID]chan struct{}
	held        map[app.PaneID]chan struct{}
	distinct    bool
	failFirst   bool
	screens     []string
	creating    chan struct{}
	createGate  chan struct{}
}

func (h *fakeHost) Capture(context.Context, app.PaneID, int) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.screens) == 0 {
		return "", nil
	}
	screen := h.screens[0]
	h.screens = h.screens[1:]
	return screen, nil
}

func (h *fakeHost) holdPane(pane app.PaneID) (release func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gates == nil {
		h.gates = map[app.PaneID]chan struct{}{}
	}
	gate := make(chan struct{})
	h.gates[pane] = gate
	if h.held == nil {
		h.held = map[app.PaneID]chan struct{}{}
	}
	h.held[pane] = make(chan struct{})
	return func() { close(gate) }
}

func (h *fakeHost) waitHeld(t *testing.T, pane app.PaneID) {
	t.Helper()
	h.mu.Lock()
	held := h.held[pane]
	h.mu.Unlock()
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatalf("no send reached held pane %s", pane)
	}
}

func (h *fakeHost) SendText(_ context.Context, pane app.PaneID, text string, bracketed bool) error {
	h.mu.Lock()
	gate := h.gates[pane]
	if held := h.held[pane]; gate != nil && held != nil {
		select {
		case <-held:
		default:
			close(held)
		}
	}
	h.mu.Unlock()
	if gate != nil {
		<-gate
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if text == h.failText {
		return errors.New("pane gone")
	}
	h.typed = append(h.typed, fmt.Sprintf("%s paste=%t %s", pane, bracketed, text))
	return nil
}

func (h *fakeHost) SendKeys(_ context.Context, pane app.PaneID, keys ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.typed = append(h.typed, fmt.Sprintf("%s keys %s", pane, strings.Join(keys, " ")))
	return nil
}

func (h *fakeHost) failOn(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failText = text
}

func (h *fakeHost) typedNow() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.typed...)
}

func (h *fakeHost) waitTyped(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := h.typedNow(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("typed %q, want %d entries", h.typedNow(), n)
	return nil
}

func (h *fakeHost) Create(_ context.Context, spec app.PaneSpec) (app.PaneID, error) {
	if h.createGate != nil {
		h.creating <- struct{}{}
		<-h.createGate
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		return "", h.err
	}
	if h.failFirst {
		h.failFirst = false
		return "", errors.New("tmux down")
	}
	h.specs = append(h.specs, spec)
	if h.distinct {
		return app.PaneID(fmt.Sprintf("%%%d", 6+len(h.specs))), nil
	}
	return "%7", nil
}

type fakeNotifier struct {
	banners  chan domain.Banner
	removals chan string
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{banners: make(chan domain.Banner, 64), removals: make(chan string, 64)}
}

func (n *fakeNotifier) Remove(_ context.Context, group string) error {
	n.removals <- group
	return nil
}

func (n *fakeNotifier) Notify(_ context.Context, b domain.Banner) error {
	n.banners <- b
	return nil
}

type fakeForeground struct{ terminal atomic.Bool }

func (f *fakeForeground) TerminalFrontmost(context.Context) bool { return f.terminal.Load() }

type fakeLister struct {
	mu       sync.Mutex
	listings map[string]domain.RepoListing
}

func (f *fakeLister) ListWorktrees(_ context.Context, dir string) (domain.RepoListing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.listings[dir]
	if !ok {
		return domain.RepoListing{}, errors.New("not a git repo")
	}
	return l, nil
}

func (f *fakeLister) set(dir string, wts ...domain.ListedWorktree) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listings[dir] = domain.RepoListing{Main: dir, Worktrees: wts}
}

type fakeFinder struct {
	mu    sync.Mutex
	prs   map[string][]domain.PullRequest
	err   error
	calls int
}

func (f *fakeFinder) PRs(_ context.Context, repos []string) (map[string][]domain.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string][]domain.PullRequest{}
	for _, r := range repos {
		if prs, ok := f.prs[r]; ok {
			out[r] = prs
		}
	}
	return out, nil
}

func (f *fakeFinder) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeFinder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeFinder) set(repo string, prs ...domain.PullRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs[repo] = prs
}

type fakeTable struct {
	mu           sync.Mutex
	listeners    []domain.Listener
	scans        int
	terminated   []int
	terminateErr error
}

func (f *fakeTable) Listeners(context.Context) ([]domain.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scans++
	return append([]domain.Listener(nil), f.listeners...), nil
}

func (f *fakeTable) Terminate(_ context.Context, pgid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.terminateErr != nil {
		return f.terminateErr
	}
	f.terminated = append(f.terminated, pgid)
	var kept []domain.Listener
	for _, l := range f.listeners {
		if l.PGID != pgid {
			kept = append(kept, l)
		}
	}
	f.listeners = kept
	return nil
}

func (f *fakeTable) failTerminate(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminateErr = err
}

func (f *fakeTable) set(ls ...domain.Listener) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listeners = ls
}

func (f *fakeTable) scanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scans
}

func (f *fakeTable) killed() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.terminated...)
}

func (h *fakeHost) Kill(_ context.Context, pane app.PaneID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killed = append(h.killed, pane)
	return nil
}

func (h *fakeHost) Show(_ context.Context, pane app.PaneID, slot app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shown = append(h.shown, shown{pane, slot})
	return nil
}

func (h *fakeHost) List(context.Context) ([]app.PaneInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listed++
	return slices.Clone(h.panes), nil
}

func (h *fakeHost) waitListed(t *testing.T) {
	t.Helper()
	waitUntil(t, "the startup pane reconcile to list panes", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.listed > 0
	})
}

type addedWorktree struct{ repo, path, branch, base string }

type fakeWorktrees struct {
	mu    sync.Mutex
	added []addedWorktree
	err   error
}

func (f *fakeWorktrees) AddWorktree(_ context.Context, repo, path, branch, base string) (app.AddedWorktree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return app.AddedWorktree{}, f.err
	}
	f.added = append(f.added, addedWorktree{repo, path, branch, base})
	return app.AddedWorktree{Main: repo, Path: path}, nil
}

type fakeReviewGit struct {
	mu    sync.Mutex
	trees map[string]string
	refs  map[string]map[string]string
	diffs map[string]string
}

func newFakeReviewGit() *fakeReviewGit {
	return &fakeReviewGit{trees: map[string]string{}, refs: map[string]map[string]string{}, diffs: map[string]string{}}
}

func (g *fakeReviewGit) WorkingTree(_ context.Context, dir string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.trees[dir]
	if !ok {
		return "", errors.New("not a git repo")
	}
	return t, nil
}

func (g *fakeReviewGit) PointRef(_ context.Context, dir, ref, tree string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refs[dir] == nil {
		g.refs[dir] = map[string]string{}
	}
	g.refs[dir][ref] = tree
	return nil
}

func (g *fakeReviewGit) TurnRefs(_ context.Context, dir string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for r := range g.refs[dir] {
		out = append(out, r)
	}
	return out, nil
}

func (g *fakeReviewGit) DeleteRefs(_ context.Context, dir string, refs []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range refs {
		delete(g.refs[dir], r)
	}
	return nil
}

func (g *fakeReviewGit) Resolve(_ context.Context, dir, rev string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.refs[dir][rev]; ok {
		return t, nil
	}
	return rev, nil
}

func (g *fakeReviewGit) MergeBase(_ context.Context, _, rev string) (string, error) {
	return "base-of-" + rev, nil
}

func (g *fakeReviewGit) Diff(_ context.Context, _, from, tree string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.diffs[from+".."+tree], nil
}

func (g *fakeReviewGit) set(f func(g *fakeReviewGit)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(g)
}

func (g *fakeReviewGit) refsOf(dir string) map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]string{}
	for k, v := range g.refs[dir] {
		out[k] = v
	}
	return out
}

type fakeTitles struct {
	mu     sync.Mutex
	title  string
	err    error
	gate   chan struct{}
	called []domain.Task
}

func (f *fakeTitles) Title(ctx context.Context, task domain.Task) (string, error) {
	f.mu.Lock()
	f.called = append(f.called, task)
	gate, title, err := f.gate, f.title, f.err
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return title, err
}

func (f *fakeTitles) calls() []domain.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Task(nil), f.called...)
}

type fakeHunkGit struct {
	mu      sync.Mutex
	calls   []string
	patches []string
}

func (g *fakeHunkGit) Stage(_ context.Context, dir, patch string) error {
	return g.record("stage "+dir, patch)
}

func (g *fakeHunkGit) Revert(_ context.Context, dir, patch string) error {
	return g.record("revert "+dir, patch)
}

func (g *fakeHunkGit) record(call, patch string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, call)
	g.patches = append(g.patches, patch)
	return nil
}

func (g *fakeHunkGit) done() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.calls...)
}

func (h *fakeClientHost) FocusBelow(context.Context, app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.focusedBelow++
	return nil
}

func (h *fakeClientHost) PopupCommand(_ context.Context, spec app.PaneSpec) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commandPopups = append(h.commandPopups, spec)
	return nil
}

func (s *memStore) DeleteSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.snap.Sessions[:0]
	for _, x := range s.snap.Sessions {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	s.snap.Sessions = kept
	events := s.snap.Events[:0]
	for _, ev := range s.snap.Events {
		if ev.SessionID != id {
			events = append(events, ev)
		}
	}
	s.snap.Events = events
}

func (h *fakeHost) SetTitle(_ context.Context, pane app.PaneID, title string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.titles == nil {
		h.titles = map[app.PaneID][]string{}
	}
	h.titles[pane] = append(h.titles[pane], title)
	return nil
}

func (h *fakeHost) title(pane app.PaneID) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ts := h.titles[pane]; len(ts) > 0 {
		return ts[len(ts)-1]
	}
	return ""
}

func (h *fakeHost) titleSets(pane app.PaneID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.titles[pane])
}

func (h *fakeClientHost) Detach(_ context.Context, slot app.Slot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.detached = append(h.detached, slot)
	return nil
}

func (h *fakeHost) Alive(_ context.Context, pane app.PaneID) (bool, error) {
	h.mu.Lock()
	during := h.duringAlive
	h.duringAlive = nil
	h.mu.Unlock()
	if during != nil {
		during()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.panes {
		if p.ID == pane {
			return p.Alive, nil
		}
	}
	return false, nil
}

func (h *fakeHost) die(pane app.PaneID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.panes {
		if h.panes[i].ID == pane {
			h.panes[i].Alive = false
		}
	}
}

type fakeRunner struct {
	mu   sync.Mutex
	runs []string
}

func (r *fakeRunner) Run(_ context.Context, dir string, argv ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, dir+": "+strings.Join(argv, " "))
	return nil
}

func (r *fakeRunner) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.runs...)
}
