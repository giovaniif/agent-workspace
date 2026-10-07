package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type WorktreeAdder interface {
	AddWorktree(ctx context.Context, repo, path, branch, base string) (AddedWorktree, error)
}

type AddedWorktree struct {
	Main string
	Path string
}

type SetupFunc func(ctx context.Context, worktree string) error

type Sessions struct {
	Host      TerminalHost
	Worktrees WorktreeAdder
	Setup     SetupFunc
	Runner    CommandRunner
}

type NewSession struct {
	ID      string
	Task    domain.Task
	Plan    domain.SessionPlan
	Harness HarnessAdapter
	Name    string
	Model   string
	Effort  string
	Prompt  string

	ProjectSetup string
}

type Started struct {
	Session  domain.Session
	Worktree *domain.Worktree
}

func (s Sessions) Start(ctx context.Context, req NewSession) (Started, error) {
	var wt *domain.Worktree
	dir := req.Plan.Dir
	if p := req.Plan.Worktree; p != nil {
		added, err := s.Worktrees.AddWorktree(ctx, p.RepoPath, p.Path, p.Branch, p.Base)
		if err != nil {
			return Started{}, fmt.Errorf("worktree %s: %w", p.Path, err)
		}
		wt = &domain.Worktree{ID: added.Path, Repo: added.Main, Path: added.Path, Branch: p.Branch}
		dir = added.Path
		if s.Setup != nil {
			if err := s.Setup(ctx, added.Path); err != nil {
				return Started{Worktree: wt}, fmt.Errorf("setup %s: %w", added.Path, err)
			}
		}
		if req.ProjectSetup != "" {
			if s.Runner == nil {
				return Started{Worktree: wt}, fmt.Errorf("project setup %s: no command runner configured", added.Path)
			}
			if err := s.Runner.Run(ctx, added.Path, "sh", "-c", req.ProjectSetup); err != nil {
				return Started{Worktree: wt}, fmt.Errorf("project setup %s: %w", added.Path, err)
			}
		}
	}
	spec := req.Harness.Launch(LaunchRequest{Name: req.Name, Dir: dir, Model: req.Model, Effort: req.Effort, Prompt: req.Prompt})
	pane, err := s.Host.Create(ctx, spec)
	if err != nil {
		return Started{Worktree: wt}, fmt.Errorf("launch %s: %w", req.Harness.Harness(), err)
	}
	session := domain.Session{
		ID:      req.ID,
		TaskID:  req.Task.ID,
		Harness: req.Harness.Harness(),
		Pane:    string(pane),
		Model:   req.Model,
		Effort:  req.Effort,
		State:   domain.StateIdle,
		Dir:     dir,
	}
	if wt != nil {
		wt.SessionID = req.ID
		session.WorktreeIDs = []string{wt.ID}
	}
	return Started{Session: session, Worktree: wt}, nil
}

func (s Sessions) End(ctx context.Context, session domain.Session) (domain.Session, error) {
	if session.Pane != "" {
		if err := s.Host.Kill(ctx, PaneID(session.Pane)); err != nil {
			return session, err
		}
	}
	return session.End(), nil
}

var ErrNotResumable = errors.New("session cannot be resumed")

func (s Sessions) Resume(ctx context.Context, session domain.Session, harness HarnessAdapter, name string) (domain.Session, error) {
	if !session.Resumable() {
		return session, ErrNotResumable
	}
	spec := harness.Launch(LaunchRequest{Name: name, Dir: session.Dir, Model: session.Model, Effort: session.Effort, Resume: session.ResumeID})
	pane, err := s.Host.Create(ctx, spec)
	if err != nil {
		return session, fmt.Errorf("resume %s: %w", harness.Harness(), err)
	}
	return session.Resumed(string(pane)), nil
}
