package tui_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

var clock = func() time.Time { return time.Date(2026, 9, 29, 21, 42, 0, 0, time.UTC) }

var fixtureStates = []domain.AgentState{
	domain.StateRunning, domain.StateIdle, domain.StateWaiting, domain.StateDone, domain.StatePermission,
}

func fixture(sessions, worktreesEach int) rpc.State {
	var st rpc.State
	for i := range sessions {
		taskID := fmt.Sprintf("t%d", i/2+1)
		if i%2 == 0 {
			st.Tasks = append(st.Tasks, domain.Task{
				ID: taskID, Source: domain.TaskText, Ref: fmt.Sprintf("#%d", 40+i/2),
				Text: fmt.Sprintf("task number %d", i/2+1),
			})
		}
		s := domain.Session{
			ID: fmt.Sprintf("s%02d", i+1), TaskID: taskID,
			Harness: domain.HarnessClaude, Model: "opus-5.5", Effort: "high",
			State: fixtureStates[i%len(fixtureStates)],
			Usage: domain.Usage{ContextLeftPercent: 64 - i, HasContext: true},
		}
		if i%2 == 1 {
			s.Harness, s.Model, s.Effort = domain.HarnessCodex, "gpt-6", "med"
		}
		s.Unread = s.State == domain.StateDone
		for w := range worktreesEach {
			wt := domain.Worktree{
				ID: fmt.Sprintf("w%02d-%d", i+1, w), Repo: []string{"api", "web", "infra"}[w%3],
				SubtaskSlug: fmt.Sprintf("part-%d", w+1),
			}
			if w == 0 {
				wt.PR = &domain.PullRequest{Number: 3600 + i, Title: fmt.Sprintf("session %d change", i+1)}
			}
			st.Worktrees = append(st.Worktrees, wt)
			s.WorktreeIDs = append(s.WorktreeIDs, wt.ID)
		}
		st.Sessions = append(st.Sessions, s)
	}
	return st
}

func newModel(st *rpc.State, f tui.Focuser) tui.Model {
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Focus: f})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	if st != nil {
		m = update(m, tui.StateMsg(*st))
	}
	return m
}

func update(m tui.Model, msg tea.Msg) tui.Model {
	next, _ := m.Update(msg)
	return next.(tui.Model)
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(m tui.Model, keys ...string) tui.Model {
	for _, k := range keys {
		m = update(m, key(k))
	}
	return m
}

func screen(m tui.Model) string { return ansi.Strip(m.View().Content) }

func goldenScreen(t *testing.T, st *rpc.State) {
	t.Helper()
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock})
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(48, 40))
	if st != nil {
		tm.Send(tui.StateMsg(*st))
	}
	tm.Send(key("q"))
	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(tui.Model)
	golden.RequireEqual(t, screen(final))
}

func TestGoldenEmpty(t *testing.T) { goldenScreen(t, nil) }

func TestGoldenOneSession(t *testing.T) {
	st := fixture(1, 3)
	goldenScreen(t, &st)
}

func TestGoldenTenSessions(t *testing.T) {
	st := fixture(10, 3)
	goldenScreen(t, &st)
}

func TestNumberKeysJumpAndTabReturnsToTheLastSession(t *testing.T) {
	st := fixture(5, 1)
	m := newModel(&st, nil)
	first := m.Selected()
	m = press(m, "3")
	third := m.Selected()
	if third == first || third == "" {
		t.Fatalf("3 selected %q; first was %q", third, first)
	}
	if m = press(m, "tab"); m.Selected() != first {
		t.Fatalf("tab selected %q, want %q", m.Selected(), first)
	}
	if m = press(m, "tab"); m.Selected() != third {
		t.Fatalf("second tab selected %q, want %q", m.Selected(), third)
	}
	if m = press(m, "9"); m.Selected() != third {
		t.Fatalf("9 with 5 sessions moved to %q", m.Selected())
	}
}

func TestJAndKMoveThroughSessionsAndStopAtTheEnds(t *testing.T) {
	st := fixture(3, 1)
	m := newModel(&st, nil)
	var order []string
	for range 4 {
		order = append(order, m.Selected())
		m = press(m, "j")
	}
	if order[0] == order[1] || order[1] == order[2] || order[2] != order[3] {
		t.Fatalf("j walked %v; want three distinct then stop", order)
	}
	m = press(m, "k", "k", "k")
	if m.Selected() != order[0] {
		t.Fatalf("k back to %q, want %q", m.Selected(), order[0])
	}
}

func TestSpaceCyclesThroughSessionsThatNeedYou(t *testing.T) {
	st := fixture(10, 1)
	m := newModel(&st, nil)
	byID := map[string]domain.Session{}
	for _, s := range st.Sessions {
		byID[s.ID] = s
	}
	seen := map[string]bool{}
	for range 8 {
		m = press(m, "space")
		if !byID[m.Selected()].NeedsYou() {
			t.Fatalf("space selected %s in state %s", m.Selected(), byID[m.Selected()].State)
		}
		seen[m.Selected()] = true
	}
	want := 0
	for _, s := range st.Sessions {
		if s.NeedsYou() {
			want++
		}
	}
	if len(seen) != want {
		t.Fatalf("space visited %d waiting sessions, want all %d", len(seen), want)
	}
}

func TestSessionsThatNeedYouSortToTheTopOfTheirTaskGroup(t *testing.T) {
	st := fixture(2, 1)
	m := newModel(&st, nil)
	s := st.Sessions[1]
	s.State = domain.StatePermission
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 1, Session: &s}))
	out := screen(m)
	if !strings.Contains(out, "✳ 1") || strings.Index(out, "CX") > strings.Index(out, "CC") {
		t.Fatalf("the codex session asking permission should be first:\n%s", out)
	}
	if !strings.Contains(out, "1 need you") {
		t.Fatalf("header does not count the session that needs you:\n%s", out)
	}
}

func TestGlyphsShowTheAgentState(t *testing.T) {
	tests := []struct {
		state  domain.AgentState
		unread bool
		glyph  string
	}{
		{domain.StateRunning, false, "◐"},
		{domain.StateWaiting, false, "✳"},
		{domain.StatePermission, false, "✳"},
		{domain.StateDone, true, "●"},
		{domain.StateDone, false, "○"},
		{domain.StateIdle, false, "○"},
	}
	for _, tt := range tests {
		st := fixture(1, 0)
		st.Sessions[0].State, st.Sessions[0].Unread = tt.state, tt.unread
		if out := screen(newModel(&st, nil)); !strings.Contains(out, tt.glyph+" 1 ") {
			t.Errorf("%s unread=%v: want glyph %s in\n%s", tt.state, tt.unread, tt.glyph, out)
		}
	}
}

func TestRunningGlyphsShareOneTicker(t *testing.T) {
	st := fixture(6, 0)
	st.Sessions[5].State = domain.StateRunning
	m := newModel(&st, nil)
	m = update(m, tui.TickMsg{})
	out := screen(m)
	if n := strings.Count(out, "◓"); n != 2 || strings.Contains(out, "◐") {
		t.Fatalf("after one tick want both running sessions on ◓, got %d:\n%s", n, out)
	}
}

func TestEnterFocusesTheAgentPane(t *testing.T) {
	st := fixture(1, 0)
	f := &fakeFocuser{}
	m := newModel(&st, f)
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	cmd()
	if f.calls != 1 {
		t.Fatalf("FocusMain called %d times", f.calls)
	}
}

func TestHelpListsTheKeyBackFromAnAgentPane(t *testing.T) {
	st := fixture(1, 0)
	out := screen(press(newModel(&st, nil), "?"))
	if !strings.Contains(out, `ctrl+\`) || !strings.Contains(out, "back to the sidebar") {
		t.Fatalf("help missing the focus-return key:\n%s", out)
	}
}

func TestQuestionMarkTogglesHelp(t *testing.T) {
	st := fixture(1, 0)
	m := press(newModel(&st, nil), "?")
	if out := screen(m); !strings.Contains(out, "next waiting") || !strings.Contains(out, "last session") {
		t.Fatalf("help missing keys:\n%s", out)
	}
	if out := screen(press(m, "?")); strings.Contains(out, "last session") {
		t.Fatalf("second ? did not close help:\n%s", out)
	}
}

func TestEmptyTopBarSlotsAreHidden(t *testing.T) {
	out := screen(newModel(nil, nil))
	top := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(top, "crew") || !strings.Contains(top, "21:42") {
		t.Fatalf("top bar = %q", top)
	}
	if strings.Contains(top, "claude") || strings.Contains(top, "codex") || strings.Contains(top, "disk") {
		t.Fatalf("empty slots shown: %q", top)
	}
	m := update(newModel(nil, nil), tui.TopBarMsg{Disk: "40G free"})
	if top := strings.SplitN(screen(m), "\n", 2)[0]; !strings.Contains(top, "disk 40G free") {
		t.Fatalf("filled disk slot missing: %q", top)
	}
}

func TestLoadThemeOverridesLatteFromConfig(t *testing.T) {
	dir := t.TempDir()
	missing, err := tui.LoadTheme(filepath.Join(dir, "none.toml"))
	if err != nil || missing != tui.Latte() {
		t.Fatalf("missing config = %+v, %v; want Latte", missing, err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[theme]\nblue = \"#112233\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	th, err := tui.LoadTheme(path)
	if err != nil {
		t.Fatal(err)
	}
	want := tui.Latte()
	want.Blue = "#112233"
	if th != want {
		t.Fatalf("theme = %+v, want %+v", th, want)
	}
	if err := os.WriteFile(path, []byte("[theme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tui.LoadTheme(path); err == nil {
		t.Fatal("broken config loaded without error")
	}
}

func BenchmarkKeypressToFrame(b *testing.B) {
	st := fixture(10, 3)
	m := newModel(&st, nil)
	keys := []tea.KeyPressMsg{key("j"), key("j"), key("space"), key("tab"), key("k"), key("5")}
	samples := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		m = update(m, keys[i%len(keys)])
		_ = m.View()
		samples = append(samples, time.Since(start))
	}
	b.StopTimer()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[len(samples)*95/100]
	b.ReportMetric(float64(p95.Microseconds())/1000, "p95-ms")
	if len(samples) >= 100 && p95 > 16*time.Millisecond {
		b.Fatalf("keypress to frame p95 %v exceeds the 16ms budget", p95)
	}
}

func TestCountsUseTheSingularForOne(t *testing.T) {
	st := fixture(1, 1)
	out := screen(newModel(&st, nil))
	for _, want := range []string{"1 worktree ", "1 session · 1 worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "1 worktrees") || strings.Contains(out, "1 sessions") {
		t.Errorf("plural after one:\n%s", out)
	}
}

func TestTaskHeaderNamesReposNotTheirPaths(t *testing.T) {
	st := rpc.State{
		Tasks:     []domain.Task{{ID: "t1", Source: domain.TaskText, Text: "add retries to upload"}},
		Worktrees: []domain.Worktree{{ID: "w1", Repo: "/Users/someone/code/platform/api", SubtaskSlug: "retries"}},
		Sessions:  []domain.Session{{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, WorktreeIDs: []string{"w1"}}},
	}
	out := screen(newModel(&st, nil))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "add retries to upload") && strings.HasSuffix(strings.TrimSpace(line), " api") {
			return
		}
	}
	t.Fatalf("no header line with the task name and the repo name:\n%s", out)
}

func TestTaskHeaderKeepsTwoReposThatShareAName(t *testing.T) {
	st := rpc.State{
		Tasks: []domain.Task{{ID: "t1", Source: domain.TaskText, Text: "same name"}},
		Worktrees: []domain.Worktree{
			{ID: "w1", Repo: "/src/api", SubtaskSlug: "a"},
			{ID: "w2", Repo: "/other/api", SubtaskSlug: "b"},
		},
		Sessions: []domain.Session{{ID: "s1", TaskID: "t1", Harness: domain.HarnessClaude, WorktreeIDs: []string{"w1", "w2"}}},
	}
	for _, line := range strings.Split(screen(newModel(&st, nil)), "\n") {
		if strings.Contains(line, "same name") && strings.HasSuffix(strings.TrimSpace(line), "api api") {
			return
		}
	}
	t.Fatalf("the header should list both repos:\n%s", screen(newModel(&st, nil)))
}

func TestSFocusesTheSelectedSessionsShell(t *testing.T) {
	st := fixture(1, 0)
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	_, cmd := m.Update(key("s"))
	if cmd == nil {
		t.Fatal("s did nothing")
	}
	cmd()
	if !slices.Equal(c.methods(), []string{rpc.MethodShellFocus}) || !reflect.DeepEqual(c.calls[0].params, rpc.ShellParams{Session: "s01"}) {
		t.Fatalf("calls %+v", c.calls)
	}
}

func TestHelpListsTheReviewKey(t *testing.T) {
	st := fixture(1, 0)
	out := screen(press(newModel(&st, nil), "?"))
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "r" && strings.Contains(line, "review") {
			return
		}
	}
	t.Fatalf("help has no line for r:\n%s", out)
}

func TestFooterListsTheMockupKeys(t *testing.T) {
	st := fixture(1, 0)
	out := screen(newModel(&st, nil))
	for _, want := range []string{"n new", "r review", "t shell", "e nvim", "? keys", "␣ next waiting"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer has no %q:\n%s", want, out)
		}
	}
}

func TestStatusBarListsThePortsInUse(t *testing.T) {
	st := fixture(2, 1)
	st.Worktrees[1].Ports = []domain.Port{{Port: 8084}, {Port: 8081}}
	lines := strings.Split(screen(newModel(&st, nil)), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "2 sessions · 2 worktrees · ports 8081 8084") {
		t.Fatalf("status bar %q; want the counts and the ports in use", last)
	}
}

func TestQDetachesFromTheLayoutInsteadOfQuitting(t *testing.T) {
	st := fixture(1, 0)
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	for _, k := range []tea.KeyPressMsg{key("q"), {Code: 'c', Mod: tea.ModCtrl}} {
		c.calls = nil
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%s did nothing", k)
		}
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatalf("%s quit the sidebar instead of detaching", k)
		}
		if !slices.Equal(c.methods(), []string{rpc.MethodClientDetach}) {
			t.Fatalf("%s calls %v", k, c.methods())
		}
	}
	c.err = &rpc.Error{Code: rpc.CodeNotFound, Message: "no client layout is open"}
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q did nothing without a layout")
	} else if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("q does not quit when there is no layout to leave")
	}
	c.err = errors.New("tmux: server exited")
	next, cmd := m.Update(key("q"))
	if _, quit := cmd().(tea.QuitMsg); quit {
		t.Fatal("q quit the sidebar on a detach error, leaving no sidebar")
	}
	if out := screen(update(next.(tui.Model), cmd())); !strings.Contains(out, "server exited") {
		t.Fatalf("the detach error is not shown:\n%s", out)
	}
}
