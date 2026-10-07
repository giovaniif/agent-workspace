package main

import (
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type nopStore struct{}

func (nopStore) PutWorkspace(domain.Workspace) {}
func (nopStore) DeleteWorkspace(string)        {}
func (nopStore) PutTask(domain.Task)           {}
func (nopStore) PutWorktree(domain.Worktree)   {}
func (nopStore) DeleteWorktree(string)         {}
func (nopStore) PutSession(domain.Session)     {}
func (nopStore) DeleteSession(string)          {}
func (nopStore) PutEvent(domain.SessionEvent)  {}
func (nopStore) PutViewed(domain.ViewedMark)   {}
func (nopStore) DeleteViewed(string)           {}
func (nopStore) PutDraft(domain.ReviewDraft)   {}
func (nopStore) PutDevice(domain.Device)       {}
func (nopStore) DeleteDevice(string)           {}
func (nopStore) PutProject(domain.Project)     {}
func (nopStore) DeleteProject(string)          {}
func (nopStore) Load() (app.Snapshot, error)   { return app.Snapshot{}, nil }
func (nopStore) Flush() error                  { return nil }
func (nopStore) Close() error                  { return nil }
