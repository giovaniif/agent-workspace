package domain

import (
	"reflect"
	"testing"
)

func TestParseWorkItem(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Task
	}{
		{
			"an issue tracker URL is plain text",
			" https://tracker.example.com/acme/issue/ENG-123/fix-the-login-bug ",
			Task{Source: TaskText, Text: "https://tracker.example.com/acme/issue/ENG-123/fix-the-login-bug"},
		},
		{
			"github PR URL",
			"https://github.com/acme/web/pull/42",
			Task{Source: TaskPR, Ref: "web#42", URL: "https://github.com/acme/web/pull/42"},
		},
		{
			"github PR URL with a trailing tab path",
			"https://github.com/acme/web/pull/42/files",
			Task{Source: TaskPR, Ref: "web#42", URL: "https://github.com/acme/web/pull/42/files"},
		},
		{"plain text", "  make the build faster ", Task{Source: TaskText, Text: "make the build faster"}},
		{"github PR URL with no number", "https://github.com/acme/web/pull/abc", Task{Source: TaskText, Text: "https://github.com/acme/web/pull/abc"}},
		{"github issue URL", "https://github.com/acme/web/issues/42", Task{Source: TaskText, Text: "https://github.com/acme/web/issues/42"}},
		{"other host", "https://example.com/acme/web/pull/42", Task{Source: TaskText, Text: "https://example.com/acme/web/pull/42"}},
		{"broken URL", "https://%zz", Task{Source: TaskText, Text: "https://%zz"}},
		{"empty", "   ", Task{Source: TaskText}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseWorkItem(c.input); got != c.want {
				t.Errorf("ParseWorkItem(%q) = %+v, want %+v", c.input, got, c.want)
			}
		})
	}
}

func TestTaskSlug(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want string
	}{
		{"pr ref", Task{Source: TaskPR, Ref: "web#42"}, "web-42"},
		{"text words", Task{Source: TaskText, Text: "Make the Build faster!"}, "make-the-build-faster"},
		{"text keeps five words", Task{Source: TaskText, Text: "one two three four five six seven"}, "one-two-three-four-five"},
		{"text drops punctuation runs", Task{Source: TaskText, Text: "--fix  the  #42 bug--"}, "fix-the-42-bug"},
		{"text is cut at 40 characters", Task{Source: TaskText, Text: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}, "abcdefghijklmnopqrstuvwxyzabcdefghijklmn"},
		{"a cut never ends on a dash", Task{Source: TaskText, Text: "abcdefghijklmnopqrstuvwxyzabcdefghijklm nop"}, "abcdefghijklmnopqrstuvwxyzabcdefghijklm"},
		{"nothing usable", Task{Source: TaskText, Text: "✨✨"}, "task"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TaskSlug(c.task); got != c.want {
				t.Errorf("TaskSlug(%+v) = %q, want %q", c.task, got, c.want)
			}
		})
	}
}

func TestFindTask(t *testing.T) {
	known := []Task{
		{ID: "1", Source: TaskPR, Ref: "api#1"},
		{ID: "2", Source: TaskPR, Ref: "web#42"},
		{ID: "3", Source: TaskText, Text: "fix it"},
	}
	cases := []struct {
		name   string
		task   Task
		wantID string
	}{
		{"same PR under another URL", Task{Source: TaskPR, Ref: "api#1", URL: "https://github.com/x/api/pull/1/files"}, "1"},
		{"same PR", Task{Source: TaskPR, Ref: "web#42"}, "2"},
		{"same text", Task{Source: TaskText, Text: "fix it"}, "3"},
		{"other text", Task{Source: TaskText, Text: "fix that"}, ""},
		{"ref under another source", Task{Source: TaskText, Text: "api#1"}, ""},
		{"empty text never matches", Task{Source: TaskText}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := FindTask(append(known, Task{ID: "4", Source: TaskText}), c.task)
			if ok != (c.wantID != "") || got.ID != c.wantID {
				t.Errorf("FindTask(%+v) = %+v, %v; want ID %q", c.task, got, ok, c.wantID)
			}
		})
	}
}

func TestPlanSessionStart(t *testing.T) {
	single := Workspace{Root: "/src/api", Kind: WorkspaceSingle, Repos: []Repo{{Name: "api", Path: "/src/api", DefaultBranch: "trunk"}}}
	cases := []struct {
		name  string
		ws    Workspace
		taken []string
		want  SessionPlan
	}{
		{
			"single repo gets a worktree from origin's default branch",
			single, nil,
			SessionPlan{Dir: "/h/worktrees/api/eng-1", Worktree: &WorktreePlan{
				Repo: "api", RepoPath: "/src/api", Path: "/h/worktrees/api/eng-1", Branch: "eng-1", Base: "origin/trunk",
			}},
		},
		{
			"a taken path gets a numbered suffix",
			single, []string{"/h/worktrees/api/eng-1", "/h/worktrees/api/eng-1-2"},
			SessionPlan{Dir: "/h/worktrees/api/eng-1-3", Worktree: &WorktreePlan{
				Repo: "api", RepoPath: "/src/api", Path: "/h/worktrees/api/eng-1-3", Branch: "eng-1-3", Base: "origin/trunk",
			}},
		},
		{
			"unknown default branch branches from HEAD",
			Workspace{Root: "/src/api", Kind: WorkspaceSingle, Repos: []Repo{{Name: "api", Path: "/src/api"}}}, nil,
			SessionPlan{Dir: "/h/worktrees/api/eng-1", Worktree: &WorktreePlan{
				Repo: "api", RepoPath: "/src/api", Path: "/h/worktrees/api/eng-1", Branch: "eng-1", Base: "HEAD",
			}},
		},
		{
			"orchestration root starts at the root with no worktree",
			Workspace{Root: "/src/shop", Kind: WorkspaceOrchestration, Repos: []Repo{{Name: "api", Path: "/src/shop/api"}}}, nil,
			SessionPlan{Dir: "/src/shop"},
		},
		{
			"single workspace with no repo falls back to the root",
			Workspace{Root: "/src/x", Kind: WorkspaceSingle}, nil,
			SessionPlan{Dir: "/src/x"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := PlanSessionStart(c.ws, "eng-1", "/h/worktrees", c.taken)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v (%+v), want %+v (%+v)", got, got.Worktree, c.want, c.want.Worktree)
			}
		})
	}
}

func TestSessionEndIdlesAndLetsGoOfThePane(t *testing.T) {
	s := Session{ID: "a", Pane: "%3", State: StatePermission, Focused: true, Unread: true, WorktreeIDs: []string{"w"}}
	got := s.End()
	want := Session{ID: "a", State: StateIdle, Ended: true, Unread: true, WorktreeIDs: []string{"w"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("End() = %+v, want %+v", got, want)
	}
	if _, ok := SessionOnPane([]Session{got}, "%3"); ok {
		t.Fatal("an ended session still owns its old pane")
	}
}

func TestSessionIsForgottenOnceEndedWithNoWorktreeLeft(t *testing.T) {
	cases := []struct {
		name string
		s    Session
		want bool
	}{
		{"live with no worktree", Session{State: StateIdle, Pane: "%1"}, false},
		{"ended with a worktree", Session{Ended: true, WorktreeIDs: []string{"w"}}, false},
		{"ended with no worktree", Session{Ended: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.Forgotten(); got != c.want {
				t.Fatalf("Forgotten() = %v, want %v", got, c.want)
			}
		})
	}
}
