package app_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

var cleanupNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func merged(path, branch string, n int) domain.Worktree {
	return domain.Worktree{ID: path, Repo: "/w/api", Path: path, Branch: branch, PR: &domain.PullRequest{Number: n, Head: branch, State: domain.PRMerged}}
}

type cleanupWorld struct {
	git   *fakeCleanupGit
	procs *fakeProcs
	trash *fakeTrash
	audit *fakeAudit
	c     *app.Cleanup
}

func newCleanupWorld(facts map[string]app.WorktreeGitFacts, holders ...map[string][]string) cleanupWorld {
	w := cleanupWorld{
		git:   &fakeCleanupGit{facts: facts, taken: map[string]bool{}},
		procs: &fakeProcs{calls: holders},
		trash: &fakeTrash{failFor: map[string]bool{}},
		audit: &fakeAudit{},
	}
	w.c = app.NewCleanup(w.git, w.procs, w.trash, w.audit, "/h/backups", func() time.Time { return cleanupNow })
	return w
}

func idle(domain.Worktree) app.SessionActivity { return app.SessionActivity{} }

func actions(ds []domain.CleanupDecision) map[string]domain.CleanupAction {
	out := map[string]domain.CleanupAction{}
	for _, d := range ds {
		out[d.Worktree.Path] = d.Action
	}
	return out
}

func outcomes(rs []app.CleanupResult) map[string]string {
	out := map[string]string{}
	for _, r := range rs {
		out[r.Decision.Worktree.Path] = r.Outcome
	}
	return out
}

func TestCleanupPlanGathersFactsPerWorktree(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{
		"/w/a": {ModifiedAt: old},
		"/w/b": {Uncommitted: 2, ModifiedAt: old},
		"/w/c": {ModifiedAt: old},
		"/w/d": {ModifiedAt: cleanupNow.Add(-time.Minute)},
		"/w/e": {ModifiedAt: old},
	}, map[string][]string{"/w/c": {"nvim (pid 7)"}})
	wts := []domain.Worktree{merged("/w/a", "a", 1), merged("/w/b", "b", 2), merged("/w/c", "c", 3), merged("/w/d", "d", 4), merged("/w/e", "e", 5), merged("/w/f", "f", 6)}
	live := func(wt domain.Worktree) app.SessionActivity {
		return app.SessionActivity{Live: wt.Path == "/w/e", LastActivity: old}
	}
	got := actions(w.c.Plan(context.Background(), wts, live))
	want := map[string]domain.CleanupAction{
		"/w/a": domain.CleanupRemove,
		"/w/b": domain.CleanupBackupThenAsk,
		"/w/c": domain.CleanupKeep,
		"/w/d": domain.CleanupKeep,
		"/w/e": domain.CleanupKeep,
		"/w/f": domain.CleanupKeep,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}
	if len(w.trash.moved) != 0 || len(w.git.ops) != 0 {
		t.Errorf("plan acted: moved %v, ops %v", w.trash.moved, w.git.ops)
	}
}

func TestCleanupPlanSessionActivityCountsAsActivity(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour)}})
	recent := func(domain.Worktree) app.SessionActivity {
		return app.SessionActivity{LastActivity: cleanupNow.Add(-time.Minute)}
	}
	got := w.c.Plan(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, recent)
	if got[0].Action != domain.CleanupKeep {
		t.Errorf("decision = %+v, want keep", got[0])
	}
}

func TestCleanupPlanKeepsAWorktreeHeldByAProject(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour)}})
	held := func(domain.Worktree) app.SessionActivity { return app.SessionActivity{Project: "shop"} }
	got := w.c.Plan(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, held)
	if got[0].Action != domain.CleanupKeep || got[0].Reason != "held by project shop" {
		t.Errorf("decision = %+v, want kept for the project", got[0])
	}
	if res := w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, held); res[0].Outcome == "removed" {
		t.Errorf("execute removed a project's worktree: %+v", res[0])
	}
}

func TestCleanupPlanKeepsEverythingWhenProcessesAreUnknown(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour)}})
	w.procs.err = errors.New("lsof missing")
	got := w.c.Plan(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle)
	if got[0].Action != domain.CleanupKeep || !strings.Contains(got[0].Reason, "lsof missing") {
		t.Errorf("decision = %+v, want keep naming the error", got[0])
	}
}

func TestCleanupExecRemovesMergedCleanIntoTrashAndPrunesOncePerRepo(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: old}, "/w/b": {ModifiedAt: old}})
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1), merged("/w/b", "b", 2)}, idle))
	if got["/w/a"] != "removed" || got["/w/b"] != "removed" {
		t.Errorf("outcomes = %v", got)
	}
	if !reflect.DeepEqual(w.trash.moved, []string{"/w/a", "/w/b"}) {
		t.Errorf("moved = %v", w.trash.moved)
	}
	if !reflect.DeepEqual(w.git.ops, []string{"prune /w/api"}) {
		t.Errorf("git ops = %v, want one prune and nothing else", w.git.ops)
	}
	if len(w.audit.records) != 2 || w.audit.records[0].Outcome != "removed" || w.audit.records[0].Path != "/w/a" || !w.audit.records[0].At.Equal(cleanupNow) {
		t.Errorf("audit = %+v", w.audit.records)
	}
}

func TestCleanupExecBacksUpDirtyAndKeepsIt(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/b": {Uncommitted: 2, ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour), Fingerprint: "f1"}})
	wts := []domain.Worktree{merged("/w/b", "b", 2)}
	got := outcomes(w.c.Execute(context.Background(), wts, idle))
	dir := "/h/backups/20260930-120000/b"
	if got["/w/b"] != "backed up to "+dir {
		t.Errorf("outcome = %q", got["/w/b"])
	}
	if !reflect.DeepEqual(w.git.ops, []string{"backup /w/b " + dir}) || len(w.trash.moved) != 0 {
		t.Errorf("ops %v, moved %v: want a backup and no removal", w.git.ops, w.trash.moved)
	}

	again := outcomes(w.c.Execute(context.Background(), wts, idle))
	if again["/w/b"] != "already backed up" || len(w.git.ops) != 1 {
		t.Errorf("second run = %q, ops %v: want no second backup of the same state", again["/w/b"], w.git.ops)
	}

	w.git.facts["/w/b"] = app.WorktreeGitFacts{Uncommitted: 3, ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour), Fingerprint: "f2"}
	w.c.Execute(context.Background(), wts, idle)
	if len(w.git.ops) != 2 {
		t.Errorf("ops %v: want a new backup once the changes moved", w.git.ops)
	}
}

func TestCleanupExecSameNamedWorktreesGetTheirOwnBackupDirs(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{
		"/w/api/agent-x": {Uncommitted: 1, ModifiedAt: old, Fingerprint: "f1"},
		"/w/web/agent-x": {Uncommitted: 1, ModifiedAt: old, Fingerprint: "f2"},
	})
	wts := []domain.Worktree{merged("/w/api/agent-x", "a", 1), merged("/w/web/agent-x", "b", 2)}
	w.c.Execute(context.Background(), wts, idle)
	w.git.facts["/w/api/agent-x"] = app.WorktreeGitFacts{Uncommitted: 2, ModifiedAt: old, Fingerprint: "f3"}
	w.c.Execute(context.Background(), wts, idle)
	dir := "/h/backups/20260930-120000/agent-x"
	want := []string{"backup /w/api/agent-x " + dir, "backup /w/web/agent-x " + dir + "-2", "backup /w/api/agent-x " + dir + "-3"}
	if !reflect.DeepEqual(w.git.ops, want) {
		t.Errorf("ops = %v, want %v", w.git.ops, want)
	}
}

func TestCleanupExecDetachedGetsBackupBranchAndIsKept(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/x": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour), Fingerprint: "f"}})
	w.git.taken["backup/wt-x"] = true
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{{ID: "/w/x", Repo: "/w/api", Path: "/w/x"}}, idle))
	dir := "/h/backups/20260930-120000/x"
	if got["/w/x"] != "backed up to "+dir+", branch backup/wt-x-2" {
		t.Errorf("outcome = %q", got["/w/x"])
	}
	want := []string{"backup /w/x " + dir, "branch /w/x backup/wt-x-2"}
	if !reflect.DeepEqual(w.git.ops, want) || len(w.trash.moved) != 0 {
		t.Errorf("ops %v, moved %v", w.git.ops, w.trash.moved)
	}
}

func TestCleanupExecFailedBackupMakesNoBranch(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/x": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour)}})
	w.git.backupErr = errors.New("disk full")
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{{ID: "/w/x", Repo: "/w/api", Path: "/w/x"}}, idle))
	if got["/w/x"] != "failed: disk full" || len(w.git.ops) != 0 || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, ops %v, moved %v", got["/w/x"], w.git.ops, w.trash.moved)
	}
}

func TestCleanupExecNeverTouchesAWorktreeHeldAtTheLastMoment(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: old}, "/w/b": {ModifiedAt: old}},
		map[string][]string{},
		map[string][]string{"/w/a": {"zsh (pid 8)"}})
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1), merged("/w/b", "b", 2)}, idle))
	if got["/w/a"] != "kept: in use by zsh (pid 8)" {
		t.Errorf("outcome = %q", got["/w/a"])
	}
	if !reflect.DeepEqual(w.trash.moved, []string{"/w/b"}) {
		t.Errorf("moved = %v, want only /w/b", w.trash.moved)
	}
}

func TestCleanupExecKeepsAWorktreeWrittenToAfterPlanning(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{
		"/w/a": {ModifiedAt: old, Fingerprint: "clean"},
		"/w/b": {ModifiedAt: old, Fingerprint: "clean"},
		"/w/c": {ModifiedAt: old, Fingerprint: "clean"},
	})
	w.git.later = map[string]app.WorktreeGitFacts{
		"/w/a": {Uncommitted: 1, ModifiedAt: old, Fingerprint: "dirty"},
		"/w/b": {ModifiedAt: old, Fingerprint: "clean"},
	}
	w.git.later["/w/c"] = app.WorktreeGitFacts{}
	w.git.facts["/w/d"] = app.WorktreeGitFacts{ModifiedAt: old, Fingerprint: "clean"}
	w.git.later["/w/d"] = app.WorktreeGitFacts{ModifiedAt: cleanupNow, Fingerprint: "clean"}
	wts := []domain.Worktree{merged("/w/a", "a", 1), merged("/w/b", "b", 2), merged("/w/c", "c", 3), merged("/w/d", "d", 4)}
	got := outcomes(w.c.Execute(context.Background(), wts, idle))
	if got["/w/a"] != "kept: changed since it was planned" || got["/w/c"] != "kept: changed since it was planned" || got["/w/d"] != "kept: changed since it was planned" {
		t.Errorf("outcomes = %v, want /w/a, /w/c and /w/d kept", got)
	}
	if !reflect.DeepEqual(w.trash.moved, []string{"/w/b"}) {
		t.Errorf("moved = %v, want only /w/b", w.trash.moved)
	}
}

func TestCleanupExecKeepsAWorktreeWhoseFactsFailAtTheLastMoment(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour), Fingerprint: "clean"}})
	w.git.failAfterFirst = true
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle))
	if got["/w/a"] != "kept: git status failed" || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, moved %v", got["/w/a"], w.trash.moved)
	}
}

func TestCleanupExecKeepsAllWhenTheLastCheckFails(t *testing.T) {
	old := cleanupNow.Add(-domain.CleanupGrace - time.Hour)
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: old}})
	w.procs.calls = []map[string][]string{{}}
	failing := &failSecond{fakeProcs: w.procs}
	c := app.NewCleanup(w.git, failing, w.trash, w.audit, "/h/backups", func() time.Time { return cleanupNow })
	got := outcomes(c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle))
	if !strings.HasPrefix(got["/w/a"], "kept: ") || len(w.trash.moved) != 0 {
		t.Errorf("outcome %q, moved %v", got["/w/a"], w.trash.moved)
	}
}

type failSecond struct{ *fakeProcs }

func (f *failSecond) Holders(ctx context.Context, paths []string) (map[string][]string, error) {
	if f.n >= 1 {
		return nil, errors.New("lsof died")
	}
	return f.fakeProcs.Holders(ctx, paths)
}

func TestCleanupExecFailedMoveIsReportedAndNotPruned(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow.Add(-domain.CleanupGrace - time.Hour)}})
	w.trash.failFor["/w/a"] = true
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle))
	if got["/w/a"] != "failed: cross-device link" || len(w.git.ops) != 0 {
		t.Errorf("outcome %q, ops %v", got["/w/a"], w.git.ops)
	}
}

func TestCleanupExecKeptWorktreesAreNotAudited(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow}})
	got := outcomes(w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle))
	if got["/w/a"] != "kept" || len(w.audit.records) != 0 {
		t.Errorf("outcome %q, audit %+v", got["/w/a"], w.audit.records)
	}
}

func TestCleanupOnlyExecutePurgesTheTrash(t *testing.T) {
	w := newCleanupWorld(map[string]app.WorktreeGitFacts{"/w/a": {ModifiedAt: cleanupNow}})
	w.c.Plan(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle)
	if w.trash.purges != 0 {
		t.Errorf("Plan purged the trash")
	}
	w.c.Execute(context.Background(), []domain.Worktree{merged("/w/a", "a", 1)}, idle)
	if w.trash.purges != 1 {
		t.Errorf("purges after Execute = %d, want 1", w.trash.purges)
	}
}
