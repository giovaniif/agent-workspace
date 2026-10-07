package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type diskEnv struct {
	path    string
	d       *daemon.Daemon
	world   *fakeCleanupWorld
	sizer   *gatedSizer
	sizes   *app.DiskSizes
	history *fakeHistory
	host    *fakeHost
}

func mergedWT(name string, n int) domain.Worktree {
	path := "/solo-" + name
	return domain.Worktree{ID: path, Repo: "/solo", Path: path, Branch: name, PR: &domain.PullRequest{Number: n, Head: name, State: domain.PRMerged}}
}

func startDisk(t *testing.T, worktrees ...domain.Worktree) diskEnv {
	t.Helper()
	store := &memStore{}
	store.snap.Worktrees = worktrees
	env := diskEnv{
		world:   &fakeCleanupWorld{dirty: map[string]int{}, holdersOf: map[string][]string{}},
		sizer:   newGatedSizer(map[string]int64{"/solo-a": 300, "/solo-b": 50, "/solo-c": 40, "/deps": 900}),
		history: &fakeHistory{records: []app.CleanupRecord{{Path: "/solo-old", Action: domain.CleanupRemove, Outcome: "removed"}}},
		host:    &fakeHost{},
	}
	env.sizes = app.NewDiskSizes(env.sizer, 2, time.Hour, time.Now)
	c := app.NewCleanup(env.world, env.world, env.world, env.world, "/h/backups", time.Now)
	env.d, env.path = start(t, store,
		daemon.WithCleanup(c, time.Hour),
		daemon.WithHarnesses(env.host),
		daemon.WithDisk(daemon.DiskDeps{Sizes: env.sizes, Volume: fakeVolume{free: 40, total: 100}, History: env.history, VolumePath: "/h", DepsStore: "/deps"}))
	t.Cleanup(func() {
		select {
		case <-env.sizer.open:
		default:
			close(env.sizer.open)
		}
		env.sizes.Wait()
	})
	return env
}

func TestDiskViewAnswersAtOnceWithSizesPendingThenFillsThemIn(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1), domain.Worktree{ID: "/solo-b", Repo: "/solo", Path: "/solo-b", Branch: "b"}, mergedWT("c", 3))
	env.world.dirty["/solo-c"] = 2
	c := dial(t, env.path)

	view, err := c.DiskView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range view.Rows {
		if r.Size != domain.SizePending {
			t.Errorf("%s size = %d before du finished, want pending", r.WorktreeID, r.Size)
		}
	}
	if view.DepsStore == nil || view.DepsStore.Path != "/deps" || view.DepsStore.Size != domain.SizePending {
		t.Errorf("deps store = %+v, want /deps pending", view.DepsStore)
	}
	if view.Free != 40 || view.Total != 100 || view.AutoCleanEvery != time.Hour {
		t.Errorf("free %d, total %d, every %v", view.Free, view.Total, view.AutoCleanEvery)
	}
	if len(view.Recent) != 1 || view.Recent[0].Path != "/solo-old" || view.Recent[0].Outcome != "removed" {
		t.Errorf("recent = %+v", view.Recent)
	}

	close(env.sizer.open)
	env.sizes.Wait()
	view, err = c.DiskView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]domain.DiskRow{}
	for _, r := range view.Rows {
		got[r.WorktreeID] = r
	}
	want := map[string]domain.DiskRow{
		"/solo-a": {WorktreeID: "/solo-a", Size: 300, Action: domain.CleanupRemove, Reason: "PR #1 merged"},
		"/solo-b": {WorktreeID: "/solo-b", Size: 50, Action: domain.CleanupKeep, Reason: "not merged"},
		"/solo-c": {WorktreeID: "/solo-c", Size: 40, Action: domain.CleanupBackupThenAsk, Reason: "2 uncommitted changes"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v\nwant   %+v", got, want)
	}
	if total, pending := domain.Reclaimable(view.Rows); total != 340 || pending != 0 {
		t.Errorf("reclaimable = %d, %d pending; want 340", total, pending)
	}
	if view.DepsStore.Size != 900 {
		t.Errorf("deps store size = %d, want 900", view.DepsStore.Size)
	}
}

func TestDiskViewDoesNotMoveOrBackUpAnything(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1), mergedWT("c", 3))
	env.world.dirty["/solo-c"] = 2
	if _, err := dial(t, env.path).DiskView(context.Background()); err != nil {
		t.Fatal(err)
	}
	if moved, backed := env.world.movedPaths(), env.world.backedUpPaths(); len(moved) != 0 || len(backed) != 0 {
		t.Errorf("view moved %v and backed up %v", moved, backed)
	}
}

func TestDiskViewNeedsCleanupAndDisk(t *testing.T) {
	_, path := start(t, &memStore{})
	var rerr *rpc.Error
	if _, err := dial(t, path).DiskView(context.Background()); !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable {
		t.Errorf("without WithDisk: %v, want unavailable", err)
	}
}

func TestCleanupWorktreeRemovesAMergedCleanOne(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1), mergedWT("b", 2))
	item, err := dial(t, env.path).CleanupWorktree(context.Background(), "/solo-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if item.Path != "/solo-a" || item.Outcome != "removed" || item.Action != domain.CleanupRemove {
		t.Errorf("item = %+v", item)
	}
	if moved := env.world.movedPaths(); !slices.Equal(moved, []string{"/solo-a"}) {
		t.Errorf("moved = %v, want only /solo-a", moved)
	}
}

func TestCleanupWorktreeLeavesADirtyOneAloneUnlessBackupIsAsked(t *testing.T) {
	env := startDisk(t, mergedWT("c", 3))
	env.world.dirty["/solo-c"] = 2
	c := dial(t, env.path)

	item, err := c.CleanupWorktree(context.Background(), "/solo-c", false)
	if err != nil {
		t.Fatal(err)
	}
	if item.Outcome != "kept: 2 uncommitted changes, back it up first" || len(env.world.movedPaths()) != 0 || len(env.world.backedUpPaths()) != 0 {
		t.Errorf("without backup: %+v, moved %v, backed up %v", item, env.world.movedPaths(), env.world.backedUpPaths())
	}

	item, err = c.CleanupWorktree(context.Background(), "/solo-c", true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(env.world.backedUpPaths(), []string{"/solo-c"}) || !slices.Equal(env.world.movedPaths(), []string{"/solo-c"}) {
		t.Errorf("with backup: %+v, backed up %v, moved %v", item, env.world.backedUpPaths(), env.world.movedPaths())
	}
}

func TestCleanupWorktreeNeverTouchesAWorktreeAProcessHolds(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1))
	env.world.holdersOf["/solo-a"] = []string{"nvim (pid 7)"}
	item, err := dial(t, env.path).CleanupWorktree(context.Background(), "/solo-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if item.Outcome != "kept: in use by nvim (pid 7)" || len(env.world.movedPaths()) != 0 {
		t.Errorf("item %+v, moved %v", item, env.world.movedPaths())
	}
}

func TestCleanupWorktreeFindsAWorktreeByPathEvenWhenItsIDDiffers(t *testing.T) {
	seeded := mergedWT("a", 1)
	seeded.ID = "wt-1"
	env := startDisk(t, seeded)
	item, err := dial(t, env.path).CleanupWorktree(context.Background(), "/solo-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if item.Outcome != "removed" || !slices.Equal(env.world.movedPaths(), []string{"/solo-a"}) {
		t.Errorf("item %+v, moved %v", item, env.world.movedPaths())
	}
}

func TestCleanupWorktreeUnknownPathIsNotFound(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1))
	var rerr *rpc.Error
	_, err := dial(t, env.path).CleanupWorktree(context.Background(), "/elsewhere", true)
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("got %v, want not_found", err)
	}
}

func TestDiskViewCarriesTheReclaimableAndWorktreeTotalsWithPendingCounts(t *testing.T) {
	env := startDisk(t, mergedWT("a", 1), domain.Worktree{ID: "/solo-b", Repo: "/solo", Path: "/solo-b", Branch: "b"}, mergedWT("c", 3))
	env.world.dirty["/solo-c"] = 2
	c := dial(t, env.path)

	view, err := c.DiskView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Reclaimable != 0 || view.ReclaimablePending != 2 || view.WorktreesSize != 0 || view.WorktreesPending != 3 {
		t.Errorf("before du: reclaimable %d (%d pending), worktrees %d (%d pending)", view.Reclaimable, view.ReclaimablePending, view.WorktreesSize, view.WorktreesPending)
	}

	close(env.sizer.open)
	env.sizes.Wait()
	view, err = c.DiskView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Reclaimable != 340 || view.ReclaimablePending != 0 || view.WorktreesSize != 390 || view.WorktreesPending != 0 {
		t.Errorf("after du: reclaimable %d (%d pending), worktrees %d (%d pending)", view.Reclaimable, view.ReclaimablePending, view.WorktreesSize, view.WorktreesPending)
	}
}
