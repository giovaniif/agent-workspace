package app_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type fakeHost struct {
	app.TerminalHost
	panes     []app.PaneInfo
	listErr   error
	created   []app.PaneSpec
	createErr error
	killed    []app.PaneID
	killErr   error
	log       *[]string
}

func (f *fakeHost) List(context.Context) ([]app.PaneInfo, error) { return f.panes, f.listErr }

func (f *fakeHost) Create(_ context.Context, spec app.PaneSpec) (app.PaneID, error) {
	if f.log != nil {
		*f.log = append(*f.log, "create "+spec.Dir)
	}
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, spec)
	return "%9", nil
}

func (f *fakeHost) Kill(_ context.Context, pane app.PaneID) error {
	if f.killErr != nil {
		return f.killErr
	}
	f.killed = append(f.killed, pane)
	return nil
}

type addedWorktree struct{ repo, path, branch, base string }

type fakeWorktrees struct {
	added []addedWorktree
	err   error
	log   *[]string
}

func (f *fakeWorktrees) AddWorktree(_ context.Context, repo, path, branch, base string) (app.AddedWorktree, error) {
	if f.log != nil {
		*f.log = append(*f.log, "add "+path)
	}
	if f.err != nil {
		return app.AddedWorktree{}, f.err
	}
	f.added = append(f.added, addedWorktree{repo, path, branch, base})
	return app.AddedWorktree{Main: "/real" + repo, Path: "/real" + path}, nil
}

type fakeHarness struct{}

func (fakeHarness) Harness() domain.Harness { return domain.HarnessCodex }

func (fakeHarness) Launch(req app.LaunchRequest) app.PaneSpec {
	command := []string{"agent", req.Model, req.Effort, req.Prompt}
	if req.Resume != "" {
		command = append(command, "resume "+req.Resume)
	}
	return app.PaneSpec{Name: req.Name, Dir: req.Dir, Command: command}
}

type fakeFS struct {
	markers  map[string]domain.GitMarker
	children map[string][]domain.Child
}

func (f fakeFS) Marker(path string) (domain.GitMarker, error) {
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

type fakeSetupWorld struct {
	recipe   *domain.Recipe
	main     string
	existing map[string]bool
	locks    map[string]domain.Lockfile
	free     []int64
	runErr   map[string]error
	ops      []string
	ticks    int
}

func (w *fakeSetupWorld) Load(string) (domain.Recipe, bool, error) {
	if w.recipe == nil {
		return domain.Recipe{}, false, nil
	}
	return *w.recipe, true, nil
}

func (w *fakeSetupWorld) MainCheckout(context.Context, string) (string, error) { return w.main, nil }

func (w *fakeSetupWorld) Exists(p string) bool { return w.existing[p] }

func (w *fakeSetupWorld) Copy(src, dst string) error {
	w.ops = append(w.ops, "copy "+src+" "+dst)
	return nil
}

func (w *fakeSetupWorld) Symlink(target, link string) error {
	w.ops = append(w.ops, "symlink "+target+" "+link)
	return nil
}

func (w *fakeSetupWorld) CloneTree(_ context.Context, src, dst string) error {
	w.ops = append(w.ops, "clone "+src+" "+dst)
	return nil
}

func (w *fakeSetupWorld) Lockfile(dir string) (domain.Lockfile, error) { return w.locks[dir], nil }

func (w *fakeSetupWorld) FreeBytes(string) (int64, error) {
	v := w.free[0]
	w.free = w.free[1:]
	return v, nil
}

func (w *fakeSetupWorld) Run(_ context.Context, dir string, argv ...string) error {
	line := "run " + strings.Join(argv, " ") + " in " + dir
	w.ops = append(w.ops, line)
	return w.runErr[strings.Join(argv, " ")]
}

func (w *fakeSetupWorld) now() time.Time {
	w.ticks++
	return time.Unix(0, 0).Add(time.Duration(w.ticks) * 1500 * time.Millisecond)
}

func (w *fakeSetupWorld) setup() app.WorktreeSetup {
	return app.WorktreeSetup{Recipes: w, FS: w, Runner: w, Git: w, Now: w.now}
}

func newWorld(recipe *domain.Recipe) *fakeSetupWorld {
	return &fakeSetupWorld{
		recipe:   recipe,
		main:     "/repos/api",
		existing: map[string]bool{},
		locks:    map[string]domain.Lockfile{},
		free:     []int64{10_000_000_000, 9_990_000_000},
	}
}

type fakeLister map[string]domain.RepoListing

func (f fakeLister) ListWorktrees(_ context.Context, dir string) (domain.RepoListing, error) {
	l, ok := f[dir]
	if !ok {
		return domain.RepoListing{}, errors.New("not a git repo")
	}
	return l, nil
}

type fakeFinder struct {
	mu    sync.Mutex
	prs   map[string][]domain.PullRequest
	err   error
	calls [][]string
}

func (f *fakeFinder) PRs(_ context.Context, repos []string) (map[string][]domain.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, repos)
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

type typingHost struct {
	app.TerminalHost
	typed   []string
	failOn  string
	failErr error
}

func (h *typingHost) SendText(_ context.Context, pane app.PaneID, text string, bracketed bool) error {
	if text == h.failOn {
		return h.failErr
	}
	h.typed = append(h.typed, fmt.Sprintf("%s paste=%t %s", pane, bracketed, text))
	return nil
}

func (h *typingHost) SendKeys(_ context.Context, pane app.PaneID, keys ...string) error {
	h.typed = append(h.typed, fmt.Sprintf("%s keys %s", pane, strings.Join(keys, " ")))
	return nil
}

type fakeReviewGit struct {
	mu         sync.Mutex
	trees      map[string]string
	refs       map[string]map[string]string
	revs       map[string]string
	mergeBases map[string]string
	diffs      map[string]string
	diffCalls  int
}

func newFakeReviewGit() *fakeReviewGit {
	return &fakeReviewGit{
		trees:      map[string]string{},
		refs:       map[string]map[string]string{},
		revs:       map[string]string{},
		mergeBases: map[string]string{},
		diffs:      map[string]string{},
	}
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
	sort.Strings(out)
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
	if h, ok := g.revs[dir+" "+rev]; ok {
		return h, nil
	}
	return "", errors.New("unknown revision " + rev)
}

func (g *fakeReviewGit) MergeBase(_ context.Context, dir, rev string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if h, ok := g.mergeBases[dir+" "+rev]; ok {
		return h, nil
	}
	return "", errors.New("no merge base with " + rev)
}

func (g *fakeReviewGit) Diff(_ context.Context, _ string, from, tree string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.diffCalls++
	return g.diffs[from+".."+tree], nil
}

type titleFake struct {
	title string
	err   error
}

func (f titleFake) Title(context.Context, domain.Task) (string, error) { return f.title, f.err }

type fakeHunkGit struct {
	staged, reverted []string
}

func (g *fakeHunkGit) Stage(_ context.Context, dir, patch string) error {
	g.staged = append(g.staged, dir+"\n"+patch)
	return nil
}

func (g *fakeHunkGit) Revert(_ context.Context, dir, patch string) error {
	g.reverted = append(g.reverted, dir+"\n"+patch)
	return nil
}

type codexPickerHost struct {
	app.TerminalHost
	models, efforts []string
	current         string
	typed           []string
	popup           []string
	highlight       int
	chosen          []string
	done            [][]string
	pending         string
	lag, stale      int
	shown           string
	captureErr      error
}

func (h *codexPickerHost) SendText(_ context.Context, _ app.PaneID, text string, _ bool) error {
	h.typed = append(h.typed, "paste "+text)
	h.pending = text
	return nil
}

func (h *codexPickerHost) SendKeys(_ context.Context, _ app.PaneID, keys ...string) error {
	h.typed = append(h.typed, "keys "+strings.Join(keys, " "))
	h.stale = h.lag
	for _, k := range keys {
		switch {
		case k == "Down" && h.highlight < len(h.popup)-1:
			h.highlight++
		case k == "Up" && h.highlight > 0:
			h.highlight--
		case k == "Escape":
			h.popup = nil
		case k == "Enter" && h.pending == "/model":
			h.pending, h.chosen = "", nil
			h.popup, h.highlight = h.models, slices.Index(h.models, h.current)
		case k == "Enter" && h.popup != nil && h.chosen == nil:
			h.chosen = []string{h.popup[h.highlight]}
			h.popup, h.highlight = h.efforts, 1
		case k == "Enter" && h.popup != nil:
			h.chosen = append(h.chosen, h.popup[h.highlight])
			h.done = append(h.done, h.chosen)
			h.current, h.popup = h.chosen[0], nil
		}
	}
	return nil
}

func (h *codexPickerHost) Capture(context.Context, app.PaneID, int) (string, error) {
	if h.captureErr != nil {
		return "", h.captureErr
	}
	if h.stale > 0 && h.shown != "" {
		h.stale--
		return h.shown, nil
	}
	h.shown = h.render()
	return h.shown, nil
}

func (h *codexPickerHost) render() string {
	var b strings.Builder
	b.WriteString("• earlier output\n  1. a numbered list\n")
	if h.popup == nil {
		b.WriteString("› ")
		return b.String()
	}
	title := "Select Model and Effort"
	if h.chosen != nil {
		title = "Select Reasoning Level for " + h.chosen[0]
	}
	b.WriteString("\n  " + title + "\n\n")
	for i, row := range h.popup {
		mark := " "
		if i == h.highlight {
			mark = "›"
		}
		fmt.Fprintf(&b, "%s %d. %s    description\n", mark, i+1, row)
	}
	b.WriteString("\n  Press enter to confirm or esc to go back")
	return b.String()
}

type fakeRunner struct {
	log *[]string
	err error
}

func (r fakeRunner) Run(_ context.Context, dir string, argv ...string) error {
	*r.log = append(*r.log, "run "+dir+" "+strings.Join(argv, " "))
	return r.err
}
