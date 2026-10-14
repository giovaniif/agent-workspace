package app_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var longIdle = cleanupNow.Add(-domain.CleanupGrace - time.Hour)

func TestCleanupRemoveWorktreeTrashesAMergedCleanOneAndAuditsIt(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: longIdle, Fingerprint: "f"}})
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, false)
	if res.Outcome != "removed" {
		t.Errorf("outcome = %q, want removed", res.Outcome)
	}
	if !reflect.DeepEqual(w.trash.moved, []string{"/w/a"}) {
		t.Errorf("moved = %v, want only /w/a", w.trash.moved)
	}
	if len(w.audit.records) != 1 || w.audit.records[0].Path != "/w/a" || w.audit.records[0].Outcome != "removed" {
		t.Errorf("audit = %+v", w.audit.records)
	}
}

func TestCleanupRemoveWorktreeWithoutBackupNeverDropsUncommittedWork(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 3, ModifiedAt: longIdle, Fingerprint: "f"}})
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, false)
	if !strings.HasPrefix(res.Outcome, "kept: ") || !strings.Contains(res.Outcome, "3 uncommitted changes") {
		t.Errorf("outcome = %q, want kept because of the 3 uncommitted changes", res.Outcome)
	}
	if len(w.trash.moved) != 0 || len(w.git.ops) != 0 {
		t.Errorf("moved %v, ops %v; a plain remove of a dirty worktree must do nothing", w.trash.moved, w.git.ops)
	}
}

func TestCleanupRemoveWorktreeKeepsWhatTheEngineKeeps(t *testing.T) {
	facts := map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: longIdle}, "/w/b": {ModifiedAt: longIdle}}
	unmerged := domain.Worktree{ID: "/w/b", Repo: "/w/api", Path: "/w/b", Branch: "b"}
	for _, backup := range []bool{false, true} {
		w := newCleanupWorld(facts, map[string][]string{"/w/a": {"nvim (pid 7)"}})
		held := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, backup)
		notMerged := w.c.RemoveWorktree(context.Background(), unmerged, idle, backup)
		if held.Outcome != "kept: in use by nvim (pid 7)" || notMerged.Outcome != "kept: not merged" {
			t.Errorf("backup=%v: outcomes %q and %q", backup, held.Outcome, notMerged.Outcome)
		}
		if len(w.trash.moved) != 0 || len(w.git.ops) != 0 {
			t.Errorf("backup=%v: moved %v, ops %v; kept worktrees must not be touched", backup, w.trash.moved, w.git.ops)
		}
	}
}

func TestCleanupRemoveWorktreeWithBackupBacksUpBeforeItMovesADirtyOne(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}})
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	dir := "/h/backups/20260930-120000/a"
	if res.Outcome != "backed up to "+dir+", removed" {
		t.Errorf("outcome = %q", res.Outcome)
	}
	if want := []string{"backup /w/a " + dir, "prune /w/api"}; !reflect.DeepEqual(w.git.ops, want) {
		t.Errorf("ops = %v, want %v", w.git.ops, want)
	}
	if !reflect.DeepEqual(w.trash.moved, []string{"/w/a"}) {
		t.Errorf("moved = %v", w.trash.moved)
	}
	if len(w.audit.records) != 1 || w.audit.records[0].Action != domain.CleanupBackupThenAsk {
		t.Errorf("audit = %+v, want one backup_then_ask record", w.audit.records)
	}
}

func TestCleanupRemoveWorktreeWithBackupKeepsTheCommitsOfADetachedHead(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/x": {ModifiedAt: longIdle, Fingerprint: "f"}})
	w.git.taken["backup/wt-x"] = true
	res := w.c.RemoveWorktree(context.Background(), domain.Worktree{ID: "/w/x", Repo: "/w/api", Path: "/w/x"}, idle, true)
	dir := "/h/backups/20260930-120000/x"
	if res.Outcome != "backed up to "+dir+", branch backup/wt-x-2, removed" {
		t.Errorf("outcome = %q", res.Outcome)
	}
	want := []string{"backup /w/x " + dir, "branch /w/x backup/wt-x-2", "prune /w/api"}
	if !reflect.DeepEqual(w.git.ops, want) {
		t.Errorf("ops = %v, want %v", w.git.ops, want)
	}
}

func TestCleanupRemoveWorktreeWithBackupOnACleanMergedOneJustRemovesIt(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: longIdle, Fingerprint: "f"}})
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if res.Outcome != "removed" || !reflect.DeepEqual(w.git.ops, []string{"prune /w/api"}) {
		t.Errorf("outcome %q, ops %v; nothing to back up", res.Outcome, w.git.ops)
	}
}

func TestCleanupRemoveWorktreeWithBackupDoesNotRemoveWhenTheBackupFails(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}})
	w.git.backupErr = errors.New("disk full")
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if res.Outcome != "failed: disk full" || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, moved %v; a failed backup must keep the worktree", res.Outcome, w.trash.moved)
	}
}

func TestCleanupRemoveWorktreeWithBackupDoesNotRemoveWhenTheBranchCannotBeMade(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/x": {ModifiedAt: longIdle, Fingerprint: "f"}})
	branchFails := &branchFailingGit{fakeCleanupGit: w.git}
	c := app.NewCleanup(branchFails, w.procs, w.trash, w.audit, "/h/backups", func() time.Time { return cleanupNow })
	res := c.RemoveWorktree(context.Background(), domain.Worktree{ID: "/w/x", Repo: "/w/api", Path: "/w/x"}, idle, true)
	if !strings.Contains(res.Outcome, "branch failed") || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, moved %v; without its branch the commits would be lost", res.Outcome, w.trash.moved)
	}
}

type branchFailingGit struct{ *fakeCleanupGit }

func (g *branchFailingGit) CreateBranch(context.Context, domain.Worktree, string) (string, error) {
	return "", errors.New("ref locked")
}

func TestCleanupRemoveWorktreeWithBackupKeepsAWorktreeSomeoneEnteredMeanwhile(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}},
		map[string][]string{},
		map[string][]string{"/w/a": {"zsh (pid 8)"}})
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if !strings.HasPrefix(res.Outcome, "backed up to ") || !strings.HasSuffix(res.Outcome, ", kept: in use by zsh (pid 8)") {
		t.Errorf("outcome = %q", res.Outcome)
	}
	if len(w.trash.moved) != 0 {
		t.Errorf("moved = %v, want none", w.trash.moved)
	}
}

func TestCleanupRemoveWorktreeWithBackupKeepsAWorktreeThatChangedAfterTheBackup(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "at backup"}})
	w.git.later = map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 3, ModifiedAt: longIdle, Fingerprint: "edited since"}}
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if !strings.HasSuffix(res.Outcome, ", kept: changed since it was backed up") || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, moved %v; work added after the backup would be lost", res.Outcome, w.trash.moved)
	}
}

func TestCleanupRemoveWorktreeWithBackupKeepsAWorktreeWhenTheLastChecksFail(t *testing.T) {
	facts := map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}}

	w := newCleanupWorld(facts)
	w.git.failAfterFirst = true
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if !strings.HasSuffix(res.Outcome, ", kept: git status failed") || len(w.trash.moved) != 0 {
		t.Errorf("git failure: outcome %q, moved %v", res.Outcome, w.trash.moved)
	}

	w = newCleanupWorld(facts)
	w.procs.calls = []map[string][]string{{}}
	c := app.NewCleanup(w.git, &failSecond{fakeProcs: w.procs}, w.trash, w.audit, "/h/backups", func() time.Time { return cleanupNow })
	res = c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if !strings.Contains(res.Outcome, ", kept: process check failed: lsof died") || len(w.trash.moved) != 0 {
		t.Errorf("lsof failure: outcome %q, moved %v", res.Outcome, w.trash.moved)
	}
}

func TestCleanupRemoveWorktreeWithBackupReportsAFailedMoveAndDoesNotPrune(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}})
	w.trash.failFor["/w/a"] = true
	res := w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if !strings.HasSuffix(res.Outcome, ", failed: cross-device link") {
		t.Errorf("outcome = %q", res.Outcome)
	}
	for _, op := range w.git.ops {
		if strings.HasPrefix(op, "prune") {
			t.Errorf("pruned after a failed move: %v", w.git.ops)
		}
	}
}

func TestCleanupRemoveWorktreeOnlyReadsTheFactsOfItsOwnWorktree(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {Uncommitted: 2, ModifiedAt: longIdle, Fingerprint: "f"}})
	w.c.RemoveWorktree(context.Background(), merged("/w/a", "a", 1), idle, true)
	if len(w.git.calls) != 1 {
		t.Errorf("git facts read for %v, want only /w/a", w.git.calls)
	}
}
