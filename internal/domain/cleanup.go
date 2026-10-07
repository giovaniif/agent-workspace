package domain

import (
	"fmt"
	"path/filepath"
	"time"
)

const CleanupGrace = 4 * time.Hour

type CleanupAction string

const (
	CleanupRemove        CleanupAction = "remove"
	CleanupBackupThenAsk CleanupAction = "backup_then_ask"
	CleanupKeep          CleanupAction = "keep"
)

type CleanupFacts struct {
	InDefault    bool
	OnDefault    bool
	Uncommitted  int
	Holders      []string
	SessionLive  bool
	LastActivity time.Time
	Unknown      string
	Project      string
}

type CleanupDecision struct {
	Worktree     Worktree
	Action       CleanupAction
	Reason       string
	BackupBranch string
}

func PlanCleanup(w Worktree, f CleanupFacts, now time.Time) CleanupDecision {
	d := CleanupDecision{Worktree: w, Action: CleanupKeep}
	prMerged := w.PR != nil && w.PR.State == PRMerged
	switch {
	case filepath.Clean(w.Path) == filepath.Clean(w.Repo):
		d.Reason = "the main checkout"
	case f.Project != "":
		d.Reason = "held by project " + f.Project
	case f.Unknown != "":
		d.Reason = "facts unavailable: " + f.Unknown
	case len(f.Holders) > 0:
		d.Reason = "in use by " + f.Holders[0]
	case f.SessionLive:
		d.Reason = "its session is live"
	case now.Sub(f.LastActivity) < CleanupGrace:
		d.Reason = fmt.Sprintf("active within the last %v", CleanupGrace)
	case f.OnDefault:
		d.Reason = "on the default branch"
	case w.Branch == "" && !f.InDefault:
		d.Action = CleanupBackupThenAsk
		d.Reason = "detached with commits not in the default branch"
		d.BackupBranch = BackupBranchName(w)
	case !prMerged && !f.InDefault:
		d.Reason = "not merged"
	case f.Uncommitted > 0:
		d.Action = CleanupBackupThenAsk
		d.Reason = fmt.Sprintf("%d uncommitted changes", f.Uncommitted)
	case prMerged:
		d.Action = CleanupRemove
		d.Reason = fmt.Sprintf("PR #%d merged", w.PR.Number)
	default:
		d.Action = CleanupRemove
		d.Reason = "merged into the default branch"
	}
	return d
}

func BackupBranchName(w Worktree) string {
	return "backup/wt-" + filepath.Base(w.Path)
}
