package git

import (
	"bytes"
	"context"
	"strings"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.WorktreeLister = Worktrees{}

func (Worktrees) ListWorktrees(ctx context.Context, dir string) (domain.RepoListing, error) {
	out, err := output(ctx, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return domain.RepoListing{}, err
	}
	return parseWorktreeList(out), nil
}

func parseWorktreeList(out []byte) domain.RepoListing {
	var listing domain.RepoListing
	var cur domain.ListedWorktree
	prunable, first, open := false, true, false
	flush := func() {
		if !open {
			return
		}
		switch {
		case first:
			listing.Main = cur.Path
			first = false
		case !prunable:
			listing.Worktrees = append(listing.Worktrees, cur)
		}
		cur, prunable, open = domain.ListedWorktree{}, false, false
	}
	for _, field := range bytes.Split(out, []byte{0}) {
		f := string(field)
		switch {
		case f == "":
			flush()
		case strings.HasPrefix(f, "worktree "):
			cur.Path = strings.TrimPrefix(f, "worktree ")
			open = true
		case strings.HasPrefix(f, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(f, "branch "), "refs/heads/")
		case strings.HasPrefix(f, "prunable"):
			prunable = true
		}
	}
	flush()
	return listing
}
