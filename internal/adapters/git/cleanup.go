package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.CleanupGit = Worktrees{}

func (Worktrees) CleanupFacts(ctx context.Context, w domain.Worktree) (app.WorktreeGitFacts, error) {
	status, err := output(ctx, w.Path, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return app.WorktreeGitFacts{}, fmt.Errorf("git status in %s: %w", w.Path, err)
	}
	branch, changed := parseStatus(status)
	h := sha256.New()
	h.Write(status)
	if changed > 0 {
		diff, err := output(ctx, w.Path, "diff", "HEAD", "--binary")
		if err != nil {
			return app.WorktreeGitFacts{}, fmt.Errorf("git diff in %s: %w", w.Path, err)
		}
		h.Write(diff)
	}
	f := app.WorktreeGitFacts{Uncommitted: changed, Fingerprint: hex.EncodeToString(h.Sum(nil))}
	if def, ok := defaultBranch(ctx, w.Path); ok {
		f.OnDefault = branch != "" && branch == def
		_, err := output(ctx, w.Path, "merge-base", "--is-ancestor", "HEAD", "refs/remotes/origin/"+def)
		f.InDefault = err == nil
	}
	f.ModifiedAt = modifiedAt(ctx, w.Path)
	return f, nil
}

func defaultBranch(ctx context.Context, dir string) (string, bool) {
	if head, err := output(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := parseDefaultBranch(string(head)); b != "" {
			return b, true
		}
	}
	for _, b := range []string{"main", "master"} {
		if _, err := output(ctx, dir, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+b); err == nil {
			return b, true
		}
	}
	return "", false
}

func modifiedAt(ctx context.Context, dir string) time.Time {
	out, err := output(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return time.Time{}
	}
	gitDir := strings.TrimSpace(string(out))
	var newest time.Time
	for _, name := range []string{"index", "HEAD"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

func (Worktrees) Backup(ctx context.Context, w domain.Worktree, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	patch, err := output(ctx, w.Path, "diff", "HEAD", "--binary")
	if err != nil {
		return fmt.Errorf("git diff in %s: %w", w.Path, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "patch.diff"), patch, 0o600); err != nil {
		return err
	}
	untracked, err := output(ctx, w.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("git ls-files in %s: %w", w.Path, err)
	}
	if len(untracked) > 0 {
		tar := exec.CommandContext(ctx, "tar", "-cf", filepath.Join(dir, "untracked.tar"), "--null", "-T", "-")
		tar.Dir = w.Path
		tar.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
		tar.Stdin = bytes.NewReader(untracked)
		if out, err := tar.CombinedOutput(); err != nil {
			return fmt.Errorf("tar untracked files in %s: %w: %s", w.Path, err, out)
		}
	}
	status, err := output(ctx, w.Path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return fmt.Errorf("git status in %s: %w", w.Path, err)
	}
	head := fmt.Sprintf("worktree %s\nrepo %s\nbranch %s\n\n", w.Path, w.Repo, w.Branch)
	return os.WriteFile(filepath.Join(dir, "status.txt"), append([]byte(head), status...), 0o600)
}

func (Worktrees) CreateBranch(ctx context.Context, w domain.Worktree, name string) (string, error) {
	for i := 1; i <= 100; i++ {
		try := name
		if i > 1 {
			try = fmt.Sprintf("%s-%d", name, i)
		}
		if _, err := output(ctx, w.Path, "rev-parse", "--verify", "-q", "refs/heads/"+try); err == nil {
			continue
		}
		if _, err := output(ctx, w.Path, "branch", try, "HEAD"); err != nil {
			return "", fmt.Errorf("git branch %s in %s: %w", try, w.Path, err)
		}
		return try, nil
	}
	return "", errors.New("no free name for " + name)
}

func (Worktrees) Prune(ctx context.Context, repo string) error {
	_, err := output(ctx, repo, "worktree", "prune")
	return err
}
