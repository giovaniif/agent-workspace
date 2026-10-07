package app_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

func newSession(plan domain.SessionPlan) app.NewSession {
	return app.NewSession{
		ID: "s1", Task: domain.Task{ID: "t1"}, Plan: plan, Harness: fakeHarness{},
		Name: "eng-1", Model: "m", Effort: "high", Prompt: "do it",
	}
}

var singlePlan = domain.SessionPlan{Dir: "/h/api/eng-1", Worktree: &domain.WorktreePlan{
	Repo: "api", RepoPath: "/src/api", Path: "/h/api/eng-1", Branch: "eng-1", Base: "origin/main",
}}

func TestStartSessionInASingleRepoAddsTheWorktreeRunsSetupThenLaunchesWhereGitPutIt(t *testing.T) {
	var log []string
	host := &fakeHost{log: &log}
	wts := &fakeWorktrees{log: &log}
	s := app.Sessions{Host: host, Worktrees: wts, Setup: func(_ context.Context, dir string) error {
		log = append(log, "setup "+dir)
		return nil
	}}
	got, err := s.Start(context.Background(), newSession(singlePlan))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"add /h/api/eng-1", "setup /real/h/api/eng-1", "create /real/h/api/eng-1"}; !slices.Equal(log, want) {
		t.Fatalf("steps %v, want %v", log, want)
	}
	if want := []addedWorktree{{"/src/api", "/h/api/eng-1", "eng-1", "origin/main"}}; !reflect.DeepEqual(wts.added, want) {
		t.Fatalf("added %+v", wts.added)
	}
	wantSpec := app.PaneSpec{Name: "eng-1", Dir: "/real/h/api/eng-1", Command: []string{"agent", "m", "high", "do it"}}
	if len(host.created) != 1 || !reflect.DeepEqual(host.created[0], wantSpec) {
		t.Fatalf("created %+v", host.created)
	}
	wantSession := domain.Session{
		ID: "s1", TaskID: "t1", Harness: domain.HarnessCodex, Pane: "%9", Model: "m", Effort: "high",
		State: domain.StateIdle, WorktreeIDs: []string{"/real/h/api/eng-1"}, Dir: "/real/h/api/eng-1",
	}
	if !reflect.DeepEqual(got.Session, wantSession) {
		t.Fatalf("session %+v", got.Session)
	}
	wantWorktree := &domain.Worktree{ID: "/real/h/api/eng-1", Repo: "/real/src/api", Path: "/real/h/api/eng-1", Branch: "eng-1", SessionID: "s1"}
	if !reflect.DeepEqual(got.Worktree, wantWorktree) {
		t.Fatalf("worktree %+v", got.Worktree)
	}
}

func TestStartSessionRunsTheProjectSetupAfterTheRecipeAndBeforeTheLaunch(t *testing.T) {
	var log []string
	s := app.Sessions{
		Host:      &fakeHost{log: &log},
		Worktrees: &fakeWorktrees{log: &log},
		Runner:    fakeRunner{log: &log},
		Setup: func(_ context.Context, dir string) error {
			log = append(log, "setup "+dir)
			return nil
		},
	}
	req := newSession(singlePlan)
	req.ProjectSetup = "make env"
	if _, err := s.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want := []string{"add /h/api/eng-1", "setup /real/h/api/eng-1", "run /real/h/api/eng-1 sh -c make env", "create /real/h/api/eng-1"}
	if !slices.Equal(log, want) {
		t.Fatalf("steps %v, want %v", log, want)
	}
}

func TestStartSessionWithoutAProjectSetupRunsNoScript(t *testing.T) {
	var log []string
	s := app.Sessions{Host: &fakeHost{log: &log}, Worktrees: &fakeWorktrees{log: &log}, Runner: fakeRunner{log: &log}}
	if _, err := s.Start(context.Background(), newSession(singlePlan)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"add /h/api/eng-1", "create /real/h/api/eng-1"}; !slices.Equal(log, want) {
		t.Fatalf("steps %v, want %v", log, want)
	}
}

func TestStartSessionStopsBeforeTheLaunchWhenTheProjectSetupFails(t *testing.T) {
	var log []string
	host := &fakeHost{log: &log}
	s := app.Sessions{Host: host, Worktrees: &fakeWorktrees{log: &log}, Runner: fakeRunner{log: &log, err: errors.New("exit 2")}}
	req := newSession(singlePlan)
	req.ProjectSetup = "false"
	got, err := s.Start(context.Background(), req)
	if err == nil || len(host.created) != 0 {
		t.Fatalf("err %v, created %+v", err, host.created)
	}
	if got.Worktree == nil || got.Worktree.Path != "/real/h/api/eng-1" {
		t.Fatalf("the worktree must be reported so it is kept: %+v", got.Worktree)
	}
}

func TestStartSessionRefusesAProjectSetupWithNoRunnerToRunIt(t *testing.T) {
	host := &fakeHost{}
	s := app.Sessions{Host: host, Worktrees: &fakeWorktrees{}}
	req := newSession(singlePlan)
	req.ProjectSetup = "make env"
	if _, err := s.Start(context.Background(), req); err == nil || len(host.created) != 0 {
		t.Fatalf("err %v, created %+v", err, host.created)
	}
}

func TestStartSessionAtAnOrchestrationRootRunsNoProjectSetup(t *testing.T) {
	var log []string
	s := app.Sessions{Host: &fakeHost{log: &log}, Worktrees: &fakeWorktrees{log: &log}, Runner: fakeRunner{log: &log}}
	req := newSession(domain.SessionPlan{Dir: "/src/shop"})
	req.ProjectSetup = "make env"
	if _, err := s.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create /src/shop"}; !slices.Equal(log, want) {
		t.Fatalf("steps %v, want %v", log, want)
	}
}

func TestStartSessionAtAnOrchestrationRootCreatesNoWorktree(t *testing.T) {
	host := &fakeHost{}
	wts := &fakeWorktrees{}
	setups := 0
	s := app.Sessions{Host: host, Worktrees: wts, Setup: func(context.Context, string) error { setups++; return nil }}
	got, err := s.Start(context.Background(), newSession(domain.SessionPlan{Dir: "/src/shop"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(wts.added) != 0 || setups != 0 || got.Worktree != nil || got.Session.WorktreeIDs != nil {
		t.Fatalf("added %v, setups %d, started %+v", wts.added, setups, got)
	}
	if len(host.created) != 1 || host.created[0].Dir != "/src/shop" || got.Session.Pane != "%9" || got.Session.Dir != "/src/shop" {
		t.Fatalf("created %+v, session %+v", host.created, got.Session)
	}
}

func TestStartSessionWithoutSetupStillLaunches(t *testing.T) {
	host := &fakeHost{}
	s := app.Sessions{Host: host, Worktrees: &fakeWorktrees{}}
	if _, err := s.Start(context.Background(), newSession(singlePlan)); err != nil || len(host.created) != 1 {
		t.Fatalf("err %v, created %d", err, len(host.created))
	}
}

func TestStartSessionStopsAtTheFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	leftover := &domain.Worktree{ID: "/real/h/api/eng-1", Repo: "/real/src/api", Path: "/real/h/api/eng-1", Branch: "eng-1"}
	cases := []struct {
		name     string
		wts      *fakeWorktrees
		setup    app.SetupFunc
		hostErr  error
		leftover *domain.Worktree
	}{
		{"worktree add", &fakeWorktrees{err: boom}, nil, nil, nil},
		{"setup", &fakeWorktrees{}, func(context.Context, string) error { return boom }, nil, leftover},
		{"pane", &fakeWorktrees{}, nil, boom, leftover},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host := &fakeHost{createErr: c.hostErr}
			s := app.Sessions{Host: host, Worktrees: c.wts, Setup: c.setup}
			got, err := s.Start(context.Background(), newSession(singlePlan))
			if !errors.Is(err, boom) || got.Session.ID != "" {
				t.Fatalf("started %+v, err %v", got, err)
			}
			if !reflect.DeepEqual(got.Worktree, c.leftover) {
				t.Fatalf("after the %s failed the worktree is %+v, want %+v unowned", c.name, got.Worktree, c.leftover)
			}
			if len(host.created) != 0 {
				t.Fatalf("a pane was created after the %s failed", c.name)
			}
		})
	}
}

func TestEndSessionKillsThePaneAndIdlesTheSession(t *testing.T) {
	host := &fakeHost{}
	s := app.Sessions{Host: host}
	running := domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning, WorktreeIDs: []string{"w"}}
	got, err := s.End(context.Background(), running)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(host.killed, []app.PaneID{"%3"}) || !reflect.DeepEqual(got, running.End()) {
		t.Fatalf("killed %v, session %+v", host.killed, got)
	}
	if _, err := s.End(context.Background(), got); err != nil || len(host.killed) != 1 {
		t.Fatalf("ending an ended session: killed %v, err %v", host.killed, err)
	}
}

func TestEndSessionKeepsTheSessionWhenThePaneWillNotDie(t *testing.T) {
	boom := errors.New("boom")
	running := domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}
	got, err := app.Sessions{Host: &fakeHost{killErr: boom}}.End(context.Background(), running)
	if !errors.Is(err, boom) || !reflect.DeepEqual(got, running) {
		t.Fatalf("session %+v, err %v", got, err)
	}
}

func TestResumeSessionRelaunchesTheHarnessInItsDirOnANewPane(t *testing.T) {
	host := &fakeHost{}
	ended := domain.Session{
		ID: "s1", TaskID: "t1", Harness: domain.HarnessCodex, Ended: true, State: domain.StateIdle,
		Model: "m", Effort: "high", ResumeID: "r1", Dir: "/h/api/eng-1", WorktreeIDs: []string{"/h/api/eng-1"},
	}
	got, err := app.Sessions{Host: host}.Resume(context.Background(), ended, fakeHarness{}, "eng-1")
	if err != nil {
		t.Fatal(err)
	}
	wantSpec := app.PaneSpec{Name: "eng-1", Dir: "/h/api/eng-1", Command: []string{"agent", "m", "high", "", "resume r1"}}
	if len(host.created) != 1 || !reflect.DeepEqual(host.created[0], wantSpec) {
		t.Fatalf("created %+v", host.created)
	}
	if want := ended.Resumed("%9"); !reflect.DeepEqual(got, want) {
		t.Fatalf("session %+v, want %+v", got, want)
	}
}

func TestResumeSessionRefusesOneThatCannotBeResumed(t *testing.T) {
	host := &fakeHost{}
	live := domain.Session{ID: "s1", Pane: "%1", ResumeID: "r1", Dir: "/w"}
	if _, err := (app.Sessions{Host: host}).Resume(context.Background(), live, fakeHarness{}, "n"); !errors.Is(err, app.ErrNotResumable) {
		t.Fatalf("err %v, want ErrNotResumable", err)
	}
	if len(host.created) != 0 {
		t.Fatalf("created %+v", host.created)
	}
}

func TestResumeSessionStaysEndedWhenThePaneFails(t *testing.T) {
	boom := errors.New("no tmux")
	ended := domain.Session{ID: "s1", Ended: true, ResumeID: "r1", Dir: "/w"}
	got, err := app.Sessions{Host: &fakeHost{createErr: boom}}.Resume(context.Background(), ended, fakeHarness{}, "n")
	if !errors.Is(err, boom) || !reflect.DeepEqual(got, ended) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
