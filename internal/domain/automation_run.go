package domain

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	AutomationTriggerManual   = "manual"
	AutomationTriggerSchedule = "schedule"
	AutomationTriggerEvent    = "event"

	AutomationSucceeded = "succeeded"
	AutomationFailed    = "failed"
	AutomationSkipped   = "skipped"
	AutomationMissed    = "missed"

	AutomationStartNew    = "new"
	AutomationStartLaunch = "launch"
	AutomationStartSend   = "send"
)

type AutomationRun struct {
	ID         string    `json:"id"`
	Automation string    `json:"automation"`
	Trigger    string    `json:"trigger"`
	Context    string    `json:"context,omitempty"`
	WorkItem   string    `json:"work_item,omitempty"`
	Repo       string    `json:"repo,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Outcome    string    `json:"outcome"`
	Reason     string    `json:"reason,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
}

type AutomationRunInput struct {
	Context  string
	WorkItem string
	Repo     string
	Event    string
	Now      time.Time
	Runs     []AutomationRun
	Live     map[string]bool
}

type AutomationStart struct {
	Kind      string
	Workspace string
	Dir       string
	Session   string
	WorkItem  string
	Prompt    string
}

func PlanAutomationRun(a Automation, in AutomationRunInput) AutomationStart {
	workItem := strings.TrimSpace(in.WorkItem)
	if workItem == "" {
		workItem = a.Name + " " + in.Now.Format("2006-01-02 15:04")
	}
	repoName := ""
	if in.Repo != "" {
		repoName = filepath.Base(in.Repo)
	}
	out := AutomationStart{
		WorkItem: workItem,
		Prompt:   ExpandPrompt(a.Prompt, PromptVars{Context: in.Context, WorkItem: workItem, Repo: repoName, Event: in.Event}),
	}
	if a.Session == AutomationSessionReuse {
		for _, r := range AutomationRunsOf(in.Runs, a.Name, 0) {
			if r.SessionID != "" && in.Live[r.SessionID] {
				out.Kind, out.Session = AutomationStartSend, r.SessionID
				return out
			}
		}
	}
	switch {
	case a.Target == AutomationTargetWorkspace:
		out.Kind, out.Dir = AutomationStartLaunch, in.Repo
	case strings.HasPrefix(a.Target, AutomationTargetWorktree):
		out.Kind, out.Dir = AutomationStartLaunch, strings.TrimPrefix(a.Target, AutomationTargetWorktree)
	default:
		out.Kind, out.Workspace = AutomationStartNew, in.Repo
	}
	return out
}

func AutomationRunsOf(runs []AutomationRun, name string, limit int) []AutomationRun {
	var out []AutomationRun
	for _, r := range runs {
		if name == "" || r.Automation == name {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(x, y AutomationRun) int { return y.Start.Compare(x.Start) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
