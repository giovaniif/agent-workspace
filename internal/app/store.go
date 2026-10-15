package app

import "github.com/giovaniif/crew/internal/domain"

const EventsPerSession = domain.SessionEventsKept

type Snapshot struct {
	Workspaces      []domain.Workspace
	Tasks           []domain.Task
	Worktrees       []domain.Worktree
	Sessions        []domain.Session
	Events          []domain.SessionEvent
	Viewed          []domain.ViewedMark
	Drafts          []domain.ReviewDraft
	Devices         []domain.Device
	Projects        []domain.Project
	AutomationRuns  []domain.AutomationRun
	AutomationTrust []domain.AutomationTrust
	MateThreads     []domain.MateThread
}

type Store interface {
	PutWorkspace(domain.Workspace)
	DeleteWorkspace(root string)
	PutTask(domain.Task)
	PutWorktree(domain.Worktree)
	DeleteWorktree(id string)
	PutSession(domain.Session)
	DeleteSession(id string)
	PutEvent(domain.SessionEvent)
	PutViewed(domain.ViewedMark)
	DeleteViewed(key string)
	PutDraft(domain.ReviewDraft)
	PutDevice(domain.Device)
	DeleteDevice(id string)
	PutProject(domain.Project)
	DeleteProject(root string)
	PutAutomationRun(domain.AutomationRun)
	PutAutomationTrust(domain.AutomationTrust)
	PutMateThread(domain.MateThread)
	Load() (Snapshot, error)
	Flush() error
	Close() error
}
