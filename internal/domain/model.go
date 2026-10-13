package domain

import "time"

type WorkspaceKind string

const (
	WorkspaceSingle        WorkspaceKind = "single"
	WorkspaceOrchestration WorkspaceKind = "orchestration"
)

type Workspace struct {
	Root     string
	Kind     WorkspaceKind
	Repos    []Repo
	LastUsed time.Time
}

type Repo struct {
	Name          string
	Path          string
	DefaultBranch string
	Branch        string
	ChangedFiles  int
}

type TaskSource string

const (
	TaskPR   TaskSource = "pr"
	TaskText TaskSource = "text"
)

type Task struct {
	ID         string
	Source     TaskSource
	Ref        string
	Text       string
	IssueTitle string
	PRTitle    string
	PinnedName string
	URL        string
}

type PRState string

const (
	PROpen   PRState = "OPEN"
	PRMerged PRState = "MERGED"
	PRClosed PRState = "CLOSED"
)

type CheckState string

const (
	CheckNone    CheckState = ""
	CheckPending CheckState = "pending"
	CheckPassing CheckState = "passing"
	CheckFailing CheckState = "failing"
)

type PullRequest struct {
	Number int
	Title  string
	URL    string
	Head   string
	Author string
	State  PRState
	Checks CheckState

	ReviewDecision    ReviewDecision
	Mergeable         Mergeable
	UnresolvedThreads int
	BotComments       int
	Failing           []FailingCheck
}

type Harness string

const (
	HarnessClaude Harness = "claude"
	HarnessCodex  Harness = "codex"
	HarnessOmp    Harness = "omp"
)

type Worktree struct {
	ID          string
	Repo        string
	Path        string
	Branch      string
	PR          *PullRequest
	SubtaskSlug string
	SessionID   string
	Ports       []Port
}

type Usage struct {
	ContextLeftPercent int
	HasContext         bool
	LimitUsedPercent   int
}

type RateLimit struct {
	Window      string
	UsedPercent int
	ResetsAt    int64
}

type StatusReport struct {
	Model       string
	Effort      string
	ContextLeft int
	HasContext  bool
	Limits      []RateLimit
	At          time.Time
}

func (s Session) Report(r StatusReport) Session {
	s = s.confirmSwitches(r)
	if r.Model != "" {
		s.Model = r.Model
	}
	if r.Effort != "" {
		s.Effort = r.Effort
	}
	if r.HasContext {
		s.Usage.ContextLeftPercent, s.Usage.HasContext = r.ContextLeft, true
	}
	if len(r.Limits) > 0 {
		s.Limits = r.Limits
		s.LimitsAt = r.At
		s.Usage.LimitUsedPercent = 0
		for _, l := range r.Limits {
			s.Usage.LimitUsedPercent = max(s.Usage.LimitUsedPercent, l.UsedPercent)
		}
	}
	return s
}
