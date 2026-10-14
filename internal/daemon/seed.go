package daemon

import (
	"fmt"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

var seedStates = []domain.AgentState{
	domain.StateRunning, domain.StateWaiting, domain.StateDone,
	domain.StateIdle, domain.StatePermission,
}

var seedRepos = []string{"api", "web", "infra"}

func seedLimits(h domain.Harness, now time.Time) []domain.RateLimit {
	resets := func(d time.Duration) int64 { return now.Add(d).Unix() }
	if h == domain.HarnessCodex {
		return []domain.RateLimit{
			{Window: "five_hour", UsedPercent: 41, ResetsAt: resets(3*time.Hour + 20*time.Minute)},
			{Window: "seven_day", UsedPercent: 12, ResetsAt: resets(5 * 24 * time.Hour)},
		}
	}
	return []domain.RateLimit{
		{Window: "five_hour", UsedPercent: 88, ResetsAt: resets(2*time.Hour + 10*time.Minute)},
		{Window: "seven_day", UsedPercent: 71, ResetsAt: resets(3 * 24 * time.Hour)},
	}
}

func seed(n int, codex bool) []Event {
	var events []Event
	now := time.Now()
	for i := range n {
		taskID := fmt.Sprintf("seed-task-%d", i/2+1)
		if i%2 == 0 {
			events = append(events, TaskChanged{Task: domain.Task{
				ID: taskID, Source: domain.TaskText, Ref: fmt.Sprintf("#%d", 40+i/2),
				Text: fmt.Sprintf("seeded task %d", i/2+1),
			}})
		}
		s := domain.Session{
			ID:      fmt.Sprintf("seed-session-%d", i+1),
			TaskID:  taskID,
			Harness: domain.HarnessClaude,
			Model:   "opus-5.5",
			Effort:  "high",
			State:   seedStates[i%len(seedStates)],
			Usage:   domain.Usage{ContextLeftPercent: 90 - (i*13)%70, HasContext: true},
		}
		if codex && i%3 == 1 {
			s.Harness, s.Model, s.Effort = domain.HarnessCodex, "gpt-6", "med"
		}
		s.Limits, s.LimitsAt = seedLimits(s.Harness, now), now
		s.Unread = s.State == domain.StateDone
		for w := range i%3 + 1 {
			repo := seedRepos[w]
			wt := domain.Worktree{
				ID:          fmt.Sprintf("seed-wt-%d-%d", i+1, w+1),
				Repo:        repo,
				Path:        fmt.Sprintf("/tmp/seed/%s-%d", repo, i+1),
				Branch:      fmt.Sprintf("seed-%d-%s", i+1, repo),
				SubtaskSlug: fmt.Sprintf("part-%d", w+1),
			}
			if w == 0 {
				wt.PR = &domain.PullRequest{Number: 100 + i, Title: fmt.Sprintf("seeded change %d", i+1)}
				if i == 0 {
					wt.PR.State, wt.PR.Checks = domain.PROpen, domain.CheckFailing
					wt.PR.ReviewDecision, wt.PR.UnresolvedThreads, wt.PR.BotComments = domain.ReviewChangesRequested, 2, 3
					wt.PR.Failing = []domain.FailingCheck{{Name: "unit tests", URL: "https://example.com/runs/1"}}
				}
			}
			events = append(events, WorktreeChanged{Worktree: wt})
			s.WorktreeIDs = append(s.WorktreeIDs, wt.ID)
		}
		events = append(events, SessionChanged{Session: s})
		for _, ev := range seedLog(s) {
			events = append(events, SessionHooked{Session: s, Event: ev})
		}
		if i == 0 {
			for _, sub := range seedSubagents(s.ID, now) {
				events = append(events, SubagentChanged{Subagent: sub})
			}
		}
	}
	return events
}

func seedLog(s domain.Session) []domain.SessionEvent {
	log := []domain.SessionEvent{
		{Kind: domain.EventUserPromptSubmit},
		{Kind: domain.EventPreToolUse, Tool: "Read", Detail: "internal/upload.go"},
		{Kind: domain.EventPreToolUse, Tool: "Edit", Detail: "internal/upload.go"},
		{Kind: domain.EventPreToolUse, Tool: "Bash", Detail: "go test ./..."},
	}
	switch s.State {
	case domain.StatePermission:
		log = append(log, domain.SessionEvent{Kind: domain.EventPermissionRequest, Tool: "Bash",
			Text: "Bash: rm -rf build\nmake clean\nmake all\nmake install"})
	case domain.StateWaiting:
		log = append(log,
			domain.SessionEvent{Kind: domain.EventStop, Text: "Tests pass.\n\nWant me to open the PR now, or wait for review?"},
			domain.SessionEvent{Kind: domain.EventWaitingForInput, Text: "Claude is waiting for your input"})
	case domain.StateDone:
		log = append(log, domain.SessionEvent{Kind: domain.EventStop, Text: "Done. The upload retries three times."})
	}
	for i := range log {
		log[i].SessionID = s.ID
	}
	return log
}

func seedSubagents(sessionID string, now time.Time) []domain.Subagent {
	return []domain.Subagent{
		{SessionID: sessionID, ID: "seed-agent-1", Type: "Explore", State: domain.SubagentStopped,
			StartedAt: now.Add(-3 * time.Minute), StoppedAt: now.Add(-2 * time.Minute), Summary: "Found 3 callers of Upload."},
		{SessionID: sessionID, ID: "seed-agent-2", Type: "Plan", State: domain.SubagentRunning, StartedAt: now.Add(-time.Minute)},
		{SessionID: sessionID, ID: "seed-agent-3", ParentID: "seed-agent-2", Type: "Reviewer", State: domain.SubagentRunning, StartedAt: now.Add(-30 * time.Second)},
	}
}
