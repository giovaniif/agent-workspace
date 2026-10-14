package domain

import (
	"testing"
)

func TestNameFor(t *testing.T) {
	pr := []PullRequest{{Number: 42, Title: "Add login"}}
	cases := []struct {
		name string
		task Task
		prs  []PullRequest
		want string
	}{
		{"pinned beats everything", Task{PinnedName: "mine", IssueTitle: "Issue", Text: "text"}, pr, "mine"},
		{"pr title beats issue", Task{IssueTitle: "Issue", Text: "text"}, pr, "Add login"},
		{"issue beats text", Task{IssueTitle: "Issue", Text: "text"}, nil, "Issue"},
		{"text last", Task{Text: "fix the thing"}, nil, "fix the thing"},
		{"blank pr title skipped", Task{Text: "text"}, []PullRequest{{Number: 1, Title: "  "}, {Number: 2, Title: "Second"}}, "Second"},
		{"blank pinned ignored", Task{PinnedName: " ", Text: "text"}, nil, "text"},
		{"blank issue ignored", Task{IssueTitle: "\t", Text: "text"}, nil, "text"},
		{"trims", Task{Text: "  spaced  "}, nil, "spaced"},
		{"nothing", Task{}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NameFor(c.task, c.prs); got != c.want {
				t.Errorf("NameFor = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNamingPrecedence(t *testing.T) {
	prs := []PullRequest{{Number: 42, Title: "Add login"}}
	task := Task{Source: TaskPR, PRTitle: "Task PR", IssueTitle: "Issue", Text: "please fix the flaky login test"}
	cases := []struct {
		name string
		task Task
		prs  []PullRequest
		want string
	}{
		{"a worktree pr beats the task's own pr title", task, prs, "Add login"},
		{"task pr title beats issue title", task, nil, "Task PR"},
		{"issue beats text", Task{IssueTitle: "Issue", Text: "some words"}, nil, "Issue"},
		{"pin beats all", Task{PinnedName: "mine", PRTitle: "Task PR"}, prs, "mine"},
		{"a pr that shows up later takes over from the issue", Task{IssueTitle: "Issue"}, prs, "Add login"},
		{"a pin survives that pr", Task{IssueTitle: "Issue", PinnedName: "mine"}, prs, "mine"},
		{"blank task pr title skipped", Task{PRTitle: " ", IssueTitle: "Issue"}, nil, "Issue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NameFor(c.task, c.prs); got != c.want {
				t.Errorf("NameFor = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNamingSummarizesTheFirstPrompt(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"short text kept", "fix the thing", "fix the thing"},
		{"cut to four words", "refactor the payment webhook retry logic", "refactor the payment webhook"},
		{"leading filler dropped", "Please fix the flaky login test when the db is slow", "fix the flaky login"},
		{"filler chain dropped", "can you add rate limiting to the api", "add rate limiting"},
		{"trailing stopwords dropped", "fix the bug in the parser", "fix the bug"},
		{"stops at the sentence end", "Fix login. Then refactor everything else", "Fix login"},
		{"first line only", "migrate the users table\nand then update the docs", "migrate the users table"},
		{"punctuation trimmed", "\"Fix\" the (auth) bug, please", "Fix the (auth) bug"},
		{"one word stays", "refactor", "refactor"},
		{"two words keep a trailing connective", "upgrade to", "upgrade to"},
		{"all filler falls back to the words", "please help me", "please help me"},
		{"blank", "  \n ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SummarizeText(c.text); got != c.want {
				t.Errorf("SummarizeText(%q) = %q, want %q", c.text, got, c.want)
			}
			if c.want == "" {
				return
			}
			if got := NameFor(Task{Text: c.text}, nil); got != c.want {
				t.Errorf("NameFor text task = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNamingPin(t *testing.T) {
	task := Task{ID: "t1", Text: "text", PinnedName: "old"}
	if got := PinName(task, "  new name ").PinnedName; got != "new name" {
		t.Errorf("pinned = %q, want the trimmed name", got)
	}
	unpinned := PinName(task, "  ")
	if unpinned.PinnedName != "" {
		t.Errorf("a blank name should unpin, got %q", unpinned.PinnedName)
	}
	if got := NameFor(unpinned, nil); got != "text" {
		t.Errorf("unpinned name = %q, want back to auto", got)
	}
	if unpinned.ID != "t1" || unpinned.Text != "text" {
		t.Errorf("pinning changed other fields: %+v", unpinned)
	}
}

func TestNamingResolvedTitle(t *testing.T) {
	cases := []struct {
		name    string
		task    Task
		title   string
		want    Task
		changed bool
	}{
		{"pr title fills the pr title", Task{Source: TaskPR}, "  Add login ", Task{Source: TaskPR, PRTitle: "Add login"}, true},
		{"same title changes nothing", Task{Source: TaskPR, PRTitle: "Same"}, "Same", Task{Source: TaskPR, PRTitle: "Same"}, false},
		{"blank title changes nothing", Task{Source: TaskPR, PRTitle: "guess"}, " ", Task{Source: TaskPR, PRTitle: "guess"}, false},
		{"an issue task from an old store has nothing to resolve", Task{Source: "linear", IssueTitle: "guess"}, "Real", Task{Source: "linear", IssueTitle: "guess"}, false},
		{"a text task has nothing to resolve", Task{Source: TaskText, Text: "x"}, "Title", Task{Source: TaskText, Text: "x"}, false},
		{"a pin is left alone", Task{Source: TaskPR, PinnedName: "mine"}, "Real", Task{Source: TaskPR, PinnedName: "mine", PRTitle: "Real"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := WithTitle(c.task, c.title)
			if got != c.want || changed != c.changed {
				t.Errorf("WithTitle = %+v, %v; want %+v, %v", got, changed, c.want, c.changed)
			}
		})
	}
}

func TestNamingFromTheFirstPrompt(t *testing.T) {
	cases := []struct {
		name    string
		task    Task
		prompt  string
		want    Task
		changed bool
	}{
		{"an empty text task takes the prompt", Task{Source: TaskText}, "  fix the flaky upload test \n", Task{Source: TaskText, Text: "fix the flaky upload test"}, true},
		{"a blank prompt changes nothing", Task{Source: TaskText}, "  ", Task{Source: TaskText}, false},
		{"a task with text keeps it", Task{Source: TaskText, Text: "mine"}, "other", Task{Source: TaskText, Text: "mine"}, false},
		{"a PR task keeps its ref", Task{Source: TaskPR, Ref: "api#1"}, "other", Task{Source: TaskPR, Ref: "api#1"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := NameFromPrompt(c.task, c.prompt)
			if got != c.want || changed != c.changed {
				t.Errorf("NameFromPrompt = %+v, %v; want %+v, %v", got, changed, c.want, c.changed)
			}
		})
	}
}
