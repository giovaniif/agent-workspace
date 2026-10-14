package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

var _ app.MainCheckouts = Worktrees{}

type Worktrees struct{}

func (Worktrees) MainCheckout(ctx context.Context, worktree string) (string, error) {
	out, err := output(ctx, worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%s: not a git worktree: %w", worktree, err)
	}
	gitDir := strings.TrimSpace(string(out))
	if filepath.Base(gitDir) != ".git" {
		return "", fmt.Errorf("%s: repo at %s has no main checkout", worktree, gitDir)
	}
	return filepath.Dir(gitDir), nil
}
