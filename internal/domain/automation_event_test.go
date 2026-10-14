package domain

import (
	"reflect"
	"strings"
	"testing"
)

func prWorktree(edit func(*PullRequest)) Worktree {
	pr := &PullRequest{Number: 42, Title: "Add login", URL: "https://github.com/o/api/pull/42", Head: "feat/login", State: PROpen, Author: "ana"}
	if edit != nil {
		edit(pr)
	}
	return Worktree{ID: "/w/api-login", Repo: "/w/api", Path: "/w/api-login", Branch: "feat/login", PR: pr}
}

func eventNames(evs []AutomationEvent) []string {
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Name)
	}
	return out
}

func TestWorktreeAutomationEvents(t *testing.T) {
	bare := Worktree{ID: "/w/api-x", Repo: "/w/api", Path: "/w/api-x", Branch: "x"}
	open := prWorktree(nil)
	failing := prWorktree(func(p *PullRequest) { p.Checks = CheckFailing })
	pending := prWorktree(func(p *PullRequest) { p.Checks = CheckPending })
	review := prWorktree(func(p *PullRequest) { p.ReviewDecision = ReviewRequired })
	merged := prWorktree(func(p *PullRequest) { p.State = PRMerged })
	other := prWorktree(func(p *PullRequest) { p.Number = 43; p.URL = "https://github.com/o/api/pull/43" })
	cases := []struct {
		name string
		old  *Worktree
		new  Worktree
		want []string
	}{
		{"new worktree", nil, bare, []string{"worktree.created"}},
		{"new worktree with an open PR", nil, open, []string{"worktree.created", "pr.opened"}},
		{"PR appears", &bare, open, []string{"pr.opened"}},
		{"a different PR appears", &open, other, []string{"pr.opened"}},
		{"no change", &open, open, []string{}},
		{"CI turns red", &pending, failing, []string{"pr.ci_failed"}},
		{"CI turns red from nothing", &open, failing, []string{"pr.ci_failed"}},
		{"CI stays red", &failing, failing, []string{}},
		{"review requested", &open, review, []string{"pr.review_requested"}},
		{"review stays requested", &review, review, []string{}},
		{"merged", &open, merged, []string{"pr.merged"}},
		{"merged stays merged", &merged, merged, []string{}},
		{"first sight of a merged PR is not opened", &bare, merged, []string{"pr.merged"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventNames(WorktreeAutomationEvents(tc.old, tc.new)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPREventCarriesTheLinkAndFailingChecks(t *testing.T) {
	failing := prWorktree(func(p *PullRequest) {
		p.Checks = CheckFailing
		p.Failing = []FailingCheck{{Name: "lint"}, {Name: "test"}}
	})
	open := prWorktree(nil)
	evs := WorktreeAutomationEvents(&open, failing)
	if len(evs) != 1 {
		t.Fatalf("events %+v", evs)
	}
	ev := evs[0]
	want := AutomationEvent{
		Name: "pr.ci_failed", Repo: "/w/api", Branch: "feat/login", Author: "ana",
		Subject: "https://github.com/o/api/pull/42", WorkItem: "https://github.com/o/api/pull/42",
	}
	text := ev.Text
	ev.Text = ""
	if !reflect.DeepEqual(ev, want) {
		t.Errorf("got %+v\nwant %+v", ev, want)
	}
	for _, s := range []string{"pr.ci_failed", "#42", "Add login", "https://github.com/o/api/pull/42", "lint", "test"} {
		if !strings.Contains(text, s) {
			t.Errorf("text %q lacks %q", text, s)
		}
	}
}

func TestSessionAutomationEvents(t *testing.T) {
	s := func(state AgentState) Session { return Session{ID: "s1", State: state, Dir: "/w/api-wt/fix-login"} }
	cases := []struct {
		name     string
		old, new Session
		want     []string
	}{
		{"finished", s(StateRunning), s(StateDone), []string{"session.finished"}},
		{"done again", s(StateDone), s(StateDone), []string{}},
		{"permission", s(StateRunning), s(StatePermission), []string{"session.needs_attention"}},
		{"waiting", s(StateRunning), s(StateWaiting), []string{"session.needs_attention"}},
		{"waiting to permission", s(StateWaiting), s(StatePermission), []string{}},
		{"running", s(StateIdle), s(StateRunning), []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evs := SessionAutomationEvents(tc.old, tc.new, "fix login", "/w/api")
			if got := eventNames(evs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, ev := range evs {
				if ev.Subject != "s1" || ev.WorkItem != "fix login" || ev.Repo != "/w/api" {
					t.Errorf("event %+v", ev)
				}
			}
		})
	}
}

func TestSessionAutomationEventsUseTheDirWithoutARepo(t *testing.T) {
	old := Session{ID: "s1", State: StateRunning, Dir: "/w/api"}
	next := Session{ID: "s1", State: StateDone, Dir: "/w/api"}
	evs := SessionAutomationEvents(old, next, "fix login", "")
	if len(evs) != 1 || evs[0].Repo != "/w/api" {
		t.Fatalf("events %+v", evs)
	}
}

func TestMatchAutomationEvent(t *testing.T) {
	ev := AutomationEvent{Name: "pr.ci_failed", Repo: "/w/api", Branch: "feat/login", Author: "ana"}
	cases := []struct {
		name string
		edit func(*Automation)
		want bool
	}{
		{"listed event", func(*Automation) {}, true},
		{"not listed", func(a *Automation) { a.On = []string{"pr.merged"} }, false},
		{"disabled", func(a *Automation) { a.Enabled = false }, false},
		{"repo by name", func(a *Automation) { a.Filter.Repo = "api" }, true},
		{"repo by path", func(a *Automation) { a.Filter.Repo = "/w/api" }, true},
		{"other repo", func(a *Automation) { a.Filter.Repo = "web" }, false},
		{"branch glob", func(a *Automation) { a.Filter.Branch = "feat/*" }, true},
		{"branch glob miss", func(a *Automation) { a.Filter.Branch = "fix/*" }, false},
		{"author", func(a *Automation) { a.Filter.Author = "ana" }, true},
		{"other author", func(a *Automation) { a.Filter.Author = "bob" }, false},
		{"repo automation sees only its repo", func(a *Automation) { a.Source = "/w/web/.crew.toml" }, false},
		{"repo automation in its repo", func(a *Automation) { a.Source = "/w/api/.crew.toml" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validAutomation()
			a.On = []string{"pr.opened", "pr.ci_failed"}
			a.Source = "/h/config.toml"
			tc.edit(&a)
			if got := MatchAutomationEvent(a, ev); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateAutomationChecksTheBranchGlob(t *testing.T) {
	a := validAutomation()
	a.Filter.Branch = "feat/["
	if err := ValidateAutomation(a); err == nil || !strings.Contains(err.Error(), "branch") {
		t.Errorf("got %v", err)
	}
}
