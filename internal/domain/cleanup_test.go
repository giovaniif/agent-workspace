package domain

import (
	"testing"
	"time"
)

func TestCleanupPlan(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	idle := now.Add(-CleanupGrace - time.Minute)
	merged := &PullRequest{Number: 42, Head: "feat", State: PRMerged}
	open := &PullRequest{Number: 43, Head: "feat", State: PROpen}
	closed := &PullRequest{Number: 44, Head: "feat", State: PRClosed}
	onBranch := func(pr *PullRequest) Worktree {
		return Worktree{ID: "/w/api-feat", Path: "/w/api-feat", Branch: "feat", PR: pr}
	}
	detached := Worktree{ID: "/w/api-x", Path: "/w/api-x"}
	cases := []struct {
		name   string
		wt     Worktree
		facts  CleanupFacts
		action CleanupAction
		reason string
		backup string
	}{
		{"merged and clean", onBranch(merged), CleanupFacts{LastActivity: idle}, CleanupRemove, "PR #42 merged", ""},
		{"reachable from default", onBranch(nil), CleanupFacts{InDefault: true, LastActivity: idle}, CleanupRemove, "merged into the default branch", ""},
		{"closed PR but reachable", onBranch(closed), CleanupFacts{InDefault: true, LastActivity: idle}, CleanupRemove, "merged into the default branch", ""},
		{"squash-merged: PR merged, HEAD not in default", onBranch(merged), CleanupFacts{InDefault: false, LastActivity: idle}, CleanupRemove, "PR #42 merged", ""},
		{"merged and dirty", onBranch(merged), CleanupFacts{Uncommitted: 3, LastActivity: idle}, CleanupBackupThenAsk, "3 uncommitted changes", ""},
		{"merged and in use", onBranch(merged), CleanupFacts{Holders: []string{"nvim (pid 7)", "zsh (pid 8)"}, LastActivity: idle}, CleanupKeep, "in use by nvim (pid 7)", ""},
		{"in use beats dirty", onBranch(merged), CleanupFacts{Uncommitted: 1, Holders: []string{"zsh (pid 8)"}, LastActivity: idle}, CleanupKeep, "in use by zsh (pid 8)", ""},
		{"merged with a live session", onBranch(merged), CleanupFacts{SessionLive: true, LastActivity: idle}, CleanupKeep, "its session is live", ""},
		{"merged but active recently", onBranch(merged), CleanupFacts{LastActivity: now.Add(-CleanupGrace + time.Minute)}, CleanupKeep, "active within the last 4h0m0s", ""},
		{"unmerged", onBranch(open), CleanupFacts{LastActivity: idle}, CleanupKeep, "not merged", ""},
		{"no PR and not in default", onBranch(nil), CleanupFacts{LastActivity: idle}, CleanupKeep, "not merged", ""},
		{"closed and not in default", onBranch(closed), CleanupFacts{LastActivity: idle}, CleanupKeep, "not merged", ""},
		{"on the default branch", Worktree{ID: "/w/api-main", Path: "/w/api-main", Branch: "main"}, CleanupFacts{OnDefault: true, InDefault: true, LastActivity: idle}, CleanupKeep, "on the default branch", ""},
		{"detached with unique commits", detached, CleanupFacts{LastActivity: idle}, CleanupBackupThenAsk, "detached with commits not in the default branch", "backup/wt-api-x"},
		{"detached, dirty, unique commits", detached, CleanupFacts{Uncommitted: 2, LastActivity: idle}, CleanupBackupThenAsk, "detached with commits not in the default branch", "backup/wt-api-x"},
		{"detached in default and clean", detached, CleanupFacts{InDefault: true, LastActivity: idle}, CleanupRemove, "merged into the default branch", ""},
		{"detached in use", detached, CleanupFacts{Holders: []string{"node (pid 9)"}, LastActivity: idle}, CleanupKeep, "in use by node (pid 9)", ""},
		{"main checkout", Worktree{ID: "/w/api", Repo: "/w/api", Path: "/w/api", Branch: "feat", PR: merged}, CleanupFacts{LastActivity: idle}, CleanupKeep, "the main checkout", ""},
		{"merged but held by a project", onBranch(merged), CleanupFacts{Project: "shop", LastActivity: idle}, CleanupKeep, "held by project shop", ""},
		{"dirty and held by a project", onBranch(merged), CleanupFacts{Project: "shop", Uncommitted: 2, LastActivity: idle}, CleanupKeep, "held by project shop", ""},
		{"detached and held by a project", detached, CleanupFacts{Project: "shop", LastActivity: idle}, CleanupKeep, "held by project shop", ""},
		{"facts unknown", onBranch(merged), CleanupFacts{Unknown: "git status failed", LastActivity: idle}, CleanupKeep, "facts unavailable: git status failed", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := PlanCleanup(c.wt, c.facts, now)
			want := CleanupDecision{Worktree: c.wt, Action: c.action, Reason: c.reason, BackupBranch: c.backup}
			if got.Worktree.ID != want.Worktree.ID || got.Action != want.Action || got.Reason != want.Reason || got.BackupBranch != want.BackupBranch {
				t.Errorf("PlanCleanup = %+v, want %+v", got, want)
			}
		})
	}
}

func TestCleanupPlanActivityExactlyAtGraceIsIdle(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	wt := Worktree{ID: "/w/a", Path: "/w/a", Branch: "a", PR: &PullRequest{Number: 1, Head: "a", State: PRMerged}}
	if got := PlanCleanup(wt, CleanupFacts{LastActivity: now.Add(-CleanupGrace)}, now); got.Action != CleanupRemove {
		t.Errorf("at the grace edge = %+v, want remove", got)
	}
}

func TestCleanupKeepsAMergedWorktreeTouchedWithinFourHours(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	wt := Worktree{ID: "/w/api-feat", Path: "/w/api-feat", Branch: "feat", PR: &PullRequest{Number: 42, Head: "feat", State: PRMerged}}
	if got := PlanCleanup(wt, CleanupFacts{LastActivity: now.Add(-3*time.Hour - 59*time.Minute)}, now); got.Action != CleanupKeep {
		t.Fatalf("touched 3h59m ago: got %v, want keep", got.Action)
	}
	if got := PlanCleanup(wt, CleanupFacts{LastActivity: now.Add(-4 * time.Hour)}, now); got.Action != CleanupRemove {
		t.Fatalf("touched 4h ago: got %v, want remove", got.Action)
	}
}
