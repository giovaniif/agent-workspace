package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

type Adder struct{}

func (Adder) AddWorktree(ctx context.Context, repo, path, branch, base string) (app.AddedWorktree, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "worktree", "add", "-q", "-b", branch, path, base)
	cmd.Env = isolated(cmd.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		return app.AddedWorktree{}, fmt.Errorf("git worktree add %s: %w: %s", branch, err, strings.TrimSpace(string(out)))
	}
	out, err := output(ctx, path, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return app.AddedWorktree{}, fmt.Errorf("%s: %w", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return app.AddedWorktree{}, fmt.Errorf("%s: unexpected rev-parse output %q", path, out)
	}
	return app.AddedWorktree{Main: filepath.Dir(lines[1]), Path: lines[0]}, nil
}
