package app

import (
	"context"
	"errors"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

type HunkGit interface {
	Stage(ctx context.Context, dir, patch string) error
	Revert(ctx context.Context, dir, patch string) error
}

var ErrUnknownHunkAction = errors.New("unknown hunk action")

func ApplyHunk(ctx context.Context, g HunkGit, dir string, f domain.FileDiff, h domain.Hunk, a domain.HunkAction) error {
	if a != domain.HunkStage && a != domain.HunkRevert {
		return ErrUnknownHunkAction
	}
	patch, err := domain.HunkPatch(f, h)
	if err != nil {
		return err
	}
	if a == domain.HunkStage {
		return g.Stage(ctx, dir, patch)
	}
	return g.Revert(ctx, dir, patch)
}

func SendPrompt(ctx context.Context, host TerminalHost, pane PaneID, prompt string, settle time.Duration) error {
	if err := host.SendText(ctx, pane, prompt, true); err != nil {
		return err
	}
	select {
	case <-time.After(settle):
	case <-ctx.Done():
		return ctx.Err()
	}
	return host.SendKeys(ctx, pane, "Enter")
}
