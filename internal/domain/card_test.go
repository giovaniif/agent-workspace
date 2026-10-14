package domain

import (
	"reflect"
	"testing"
	"time"
)

var cardTask = Task{ID: "t1", Source: TaskPR, Ref: "#42", PRTitle: "Retry failed uploads"}

func fixtureLog(sessionID string) []SessionEvent {
	at := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	tool := func(name, detail string) []SessionEvent {
		return []SessionEvent{
			{Kind: EventPreToolUse, Tool: name, Detail: detail},
			{Kind: EventPostToolUse, Tool: name, Detail: detail},
		}
	}
	log := []SessionEvent{{Kind: EventSessionStart}, {Kind: EventUserPromptSubmit}}
	log = append(log, tool("Read", "internal/upload.go")...)
	log = append(log, tool("Edit", "internal/upload.go")...)
	log = append(log, tool("Bash", "go test ./...")...)
	log = append(log, tool("Bash", "git status")...)
	for i := range log {
		log[i].SessionID = sessionID
		log[i].At = at.Add(time.Duration(i) * time.Second)
	}
	return log
}

func withEvent(log []SessionEvent, ev SessionEvent) []SessionEvent {
	ev.SessionID = log[0].SessionID
	return append(log[:len(log):len(log)], ev)
}

func TestSessionCardShowsTheTaskAndItsPullRequests(t *testing.T) {
	worktrees := []Worktree{
		{ID: "w1", PR: &PullRequest{Number: 7, Title: "api"}},
		{ID: "w2"},
		{ID: "w3", PR: &PullRequest{Number: 9, Title: "web"}},
		{ID: "w4", PR: &PullRequest{Number: 7, Title: "api"}},
	}
	card := BuildSessionCard(cardTask, Session{ID: "s1"}, worktrees, nil)
	if card.Title != "Retry failed uploads" || card.Ref != "#42" {
		t.Fatalf("title %q, ref %q", card.Title, card.Ref)
	}
	want := []PullRequest{{Number: 7, Title: "api"}, {Number: 9, Title: "web"}}
	if !reflect.DeepEqual(card.PRs, want) {
		t.Fatalf("PRs = %v, want %v", card.PRs, want)
	}
}

func TestSessionCardListsTheLastThreeToolCallsNewestFirst(t *testing.T) {
	card := BuildSessionCard(cardTask, Session{ID: "s1"}, nil, fixtureLog("s1"))
	want := []string{"git status", "go test ./...", "Edit upload.go"}
	if !reflect.DeepEqual(card.Actions, want) {
		t.Fatalf("actions = %q, want %q", card.Actions, want)
	}
}

func TestSessionCardActionWithoutDetailIsJustTheTool(t *testing.T) {
	log := withEvent(fixtureLog("s1"), SessionEvent{Kind: EventPreToolUse, Tool: "ExitPlanMode"})
	if got := BuildSessionCard(cardTask, Session{ID: "s1"}, nil, log).Actions[0]; got != "ExitPlanMode" {
		t.Fatalf("action = %q", got)
	}
}

func TestSessionCardActionsNameTheCommandOrTheFile(t *testing.T) {
	tests := []struct {
		name, tool, detail, want string
	}{
		{"a shell command is the command", "Bash", "gh pr create", "gh pr create"},
		{"a file tool keeps the file name only", "Write", "/tmp/claude-1000/scratch/project/Main.dc.html", "Write Main.dc.html"},
		{"a relative path keeps the file name only", "Read", "internal/tui/view.go", "Read view.go"},
		{"other tools keep their detail", "Grep", "func main", "Grep: func main"},
		{"a shell call without a command is the tool", "Bash", "", "Bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := withEvent(fixtureLog("s1"), SessionEvent{Kind: EventPreToolUse, Tool: tt.tool, Detail: tt.detail})
			if got := BuildSessionCard(cardTask, Session{ID: "s1"}, nil, log).Actions[0]; got != tt.want {
				t.Fatalf("action = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionCardIgnoresOtherSessionsEvents(t *testing.T) {
	log := append(fixtureLog("s1"), SessionEvent{SessionID: "other", Kind: EventPreToolUse, Tool: "Bash", Detail: "rm"})
	if got := BuildSessionCard(cardTask, Session{ID: "s1"}, nil, log).Actions[0]; got != "git status" {
		t.Fatalf("action = %q", got)
	}
}

func TestSessionCardPermissionTextIsVerbatimAndCutToThreeLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"short", "Bash: make", "Bash: make"},
		{"three lines stay whole", "a\n  b\nc", "a\n  b\nc"},
		{"the fourth line is dropped and marked", "a\nb\nc\nd\ne", "a\nb\nc…"},
		{"blank lines count", "a\n\n\nd", "a\n\n…"},
		{"trailing newline is not a line", "a\nb\nc\n", "a\nb\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := withEvent(fixtureLog("s1"), SessionEvent{Kind: EventPermissionRequest, Text: tt.text})
			got := BuildSessionCard(cardTask, Session{ID: "s1", State: StatePermission}, nil, log).Waiting
			if got != tt.want {
				t.Fatalf("waiting = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionCardPermissionUsesTheLatestRequest(t *testing.T) {
	log := withEvent(fixtureLog("s1"), SessionEvent{Kind: EventPermissionRequest, Text: "old"})
	log = withEvent(log, SessionEvent{Kind: EventPermissionRequest, Text: "new"})
	if got := BuildSessionCard(cardTask, Session{ID: "s1", State: StatePermission}, nil, log).Waiting; got != "new" {
		t.Fatalf("waiting = %q", got)
	}
}

func TestSessionCardWaitingShowsTheLastAssistantQuestion(t *testing.T) {
	tests := []struct {
		name  string
		state AgentState
		extra []SessionEvent
		want  string
	}{
		{
			name:  "the closing question of the last message",
			state: StateWaiting,
			extra: []SessionEvent{
				{Kind: EventStop, Text: "Tests pass.\n\nWant me to open the PR now,\nor wait for review?"},
				{Kind: EventWaitingForInput, Text: "Claude is waiting for your input"},
			},
			want: "Want me to open the PR now,\nor wait for review?",
		},
		{
			name:  "no question falls back to the notification",
			state: StateWaiting,
			extra: []SessionEvent{
				{Kind: EventStop, Text: "All done."},
				{Kind: EventWaitingForInput, Text: "Claude is waiting for your input"},
			},
			want: "Claude is waiting for your input",
		},
		{
			name:  "done with a question is still waiting on you",
			state: StateDone,
			extra: []SessionEvent{{Kind: EventStop, Text: "Which branch?"}},
			want:  "Which branch?",
		},
		{
			name:  "done without a question waits on nothing",
			state: StateDone,
			extra: []SessionEvent{{Kind: EventStop, Text: "Shipped."}},
			want:  "",
		},
		{
			name:  "a question from before the last prompt is stale",
			state: StateWaiting,
			extra: []SessionEvent{
				{Kind: EventStop, Text: "Which branch?"},
				{Kind: EventUserPromptSubmit},
				{Kind: EventWaitingForInput, Text: "Claude is waiting for your input"},
			},
			want: "Claude is waiting for your input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := fixtureLog("s1")
			for _, ev := range tt.extra {
				log = withEvent(log, ev)
			}
			got := BuildSessionCard(cardTask, Session{ID: "s1", State: tt.state}, nil, log).Waiting
			if got != tt.want {
				t.Fatalf("waiting = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionCardRunningOrIdleWaitsOnNothing(t *testing.T) {
	log := withEvent(fixtureLog("s1"), SessionEvent{Kind: EventPermissionRequest, Text: "Bash: make"})
	for _, st := range []AgentState{StateRunning, StateIdle} {
		if got := BuildSessionCard(cardTask, Session{ID: "s1", State: st}, nil, log).Waiting; got != "" {
			t.Errorf("%s waiting = %q", st, got)
		}
	}
}

func TestSessionCardCarriesWhatTheAgentLastSaid(t *testing.T) {
	log := []SessionEvent{
		{SessionID: "s", Kind: EventStop, Text: "First answer."},
		{SessionID: "s", Kind: EventUserPromptSubmit},
		{SessionID: "s", Kind: EventPreToolUse, Tool: "Edit"},
		{SessionID: "s", Kind: EventStop, Text: "one\ntwo\nthree\nfour\nfive\nsix"},
		{SessionID: "other", Kind: EventStop, Text: "not this session"},
	}
	card := BuildSessionCard(Task{}, Session{ID: "s", State: StateDone}, nil, log)
	if card.Said != "one\ntwo\nthree\nfour…" {
		t.Fatalf("said %q; want the latest stop text cut to %d lines", card.Said, CardSaidLines)
	}
	if BuildSessionCard(Task{}, Session{ID: "s"}, nil, log[1:3]).Said != "" {
		t.Fatal("a session that has not stopped yet said something")
	}
}
