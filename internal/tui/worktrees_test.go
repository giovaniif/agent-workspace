package tui_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

const (
	gb = 1_000_000_000
	mb = 1_000_000
)

func diskRows() []domain.DiskRow {
	return []domain.DiskRow{
		{WorktreeID: "w01-0", Size: 2 * gb, Action: domain.CleanupRemove, Reason: "PR #3600 merged"},
		{WorktreeID: "w01-1", Size: 500 * mb, Action: domain.CleanupBackupThenAsk, Reason: "2 uncommitted changes"},
		{WorktreeID: "w02-0", Size: 1500 * mb, Action: domain.CleanupKeep, Reason: "its session is live"},
		{WorktreeID: "w02-1", Size: domain.SizePending, Action: domain.CleanupKeep, Reason: "not merged"},
	}
}

func diskView(rows ...domain.DiskRow) rpc.DiskView {
	return rpc.DiskView{
		Free: 240 * gb, Total: 994 * gb, AutoCleanEvery: 10 * time.Minute,
		DepsStore: &rpc.DepsStore{Path: "/deps", Size: 4100 * mb},
		Rows:      rows,
		Recent: []rpc.RecentCleanup{
			{At: time.Date(2026, 9, 29, 12, 3, 0, 0, time.UTC), Path: "/wt/old-merged", Action: domain.CleanupRemove, Outcome: "removed"},
			{At: time.Date(2026, 9, 29, 11, 40, 0, 0, time.UTC), Path: "/wt/old-dirty", Action: domain.CleanupBackupThenAsk, Outcome: "backed up to /h/backups/x"},
		},
	}
}

type diskRig struct {
	m        tui.Model
	disk     *fakeDisker
	killer   *fakeKiller
	attender *fakeAttender
	focuser  *fakeFocuser
}

func diskModel(views ...rpc.DiskView) *diskRig {
	st := fixture(2, 2)
	branches := []string{"feat-a", "feat-b", "feat-c", "feat-d"}
	for i := range st.Worktrees {
		st.Worktrees[i].Path = "/wt/" + st.Worktrees[i].ID
		st.Worktrees[i].Branch = branches[i]
		st.Worktrees[i].SessionID = "s" + st.Worktrees[i].ID[1:3]
	}
	st.Worktrees[0].PR.State = domain.PRMerged
	st.Worktrees[2].Ports = []domain.Port{{Port: 3000, PID: 201, PGID: 200, Command: "bun"}}
	if len(views) == 0 {
		views = []rpc.DiskView{diskView(diskRows()...)}
	}
	r := &diskRig{disk: &fakeDisker{views: views, item: rpc.CleanupItem{Outcome: "removed"}}, killer: &fakeKiller{}, attender: &fakeAttender{}, focuser: &fakeFocuser{}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Disk: r.disk, Kill: r.killer, Attend: r.attender, Focus: r.focuser})
	m = update(m, tea.WindowSizeMsg{Width: 150, Height: 40})
	r.m = update(m, tui.StateMsg(st))
	return r
}

func (r *diskRig) open() *diskRig {
	r.m = drive(r.m, key("w"))
	return r
}

func (r *diskRig) press(ks ...string) *diskRig {
	r.m = drive(r.m, keys(ks...)...)
	return r
}

func rowOf(t *testing.T, out, branch string) string {
	t.Helper()
	return lineWith(t, out, branch)
}

func TestWorktreesKeyOpensTheViewWithoutTheKeyPressWaitingOnTheDaemon(t *testing.T) {
	r := diskModel()
	next, cmd := r.m.Update(key("w"))
	if r.disk.fetched() != 0 {
		t.Fatal("the key press itself called the daemon; it must only return a command")
	}
	if cmd == nil {
		t.Fatal("no command to fetch the view")
	}
	if out := screen(next.(tui.Model)); !strings.Contains(out, "WORKTREES & DISK") || !strings.Contains(out, "loading") {
		t.Errorf("view before the answer:\n%s", out)
	}
	r.open()
	if !reflect.DeepEqual(r.disk.layouts, []bool{true}) || r.disk.fetched() != 1 {
		t.Errorf("layouts %v, fetches %d; want the sidebar widened and one fetch", r.disk.layouts, r.disk.fetched())
	}
}

func TestWorktreesHeaderShowsVolumeTotalsReclaimablePolicyAndDepsStore(t *testing.T) {
	out := screen(diskModel().open().m)
	for _, want := range []string{"WORKTREES & DISK", "Disk", "240 GB free of 994 GB", "worktrees 4.0 GB+", "reclaimable 2.5 GB", "other",
		"Auto-cleanup every 10m", "merged + clean + no session → removed", "merged + dirty → backup, then asks",
		"Shared deps store", "deps 4.1 GB once, cloned per worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("header lacks %q:\n%s", want, out)
		}
	}
}

func TestWorktreesTableHasStateTaskRepoBranchPRSessionPortAndSize(t *testing.T) {
	out := screen(diskModel().open().m)
	header := lineWith(t, out, "STATE")
	for _, col := range []string{"TASK", "REPO", "BRANCH", "PR", "SESSION", "PORT", "SIZE"} {
		if !strings.Contains(header, col) {
			t.Errorf("column header %q lacks %s", header, col)
		}
	}
	merged := rowOf(t, out, "feat-a")
	for _, want := range []string{"✓ merged", "#40 · task number 1", "api", "#3600", "1 claude", "2.0 GB"} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged row %q lacks %q", merged, want)
		}
	}
	live := rowOf(t, out, "feat-c")
	for _, want := range []string{"○ idle", ":3000", "1.5 GB", "2 codex"} {
		if !strings.Contains(live, want) {
			t.Errorf("live row %q lacks %q", live, want)
		}
	}
	if dirty := rowOf(t, out, "feat-b"); !strings.Contains(dirty, "! merged, dirty") || !strings.Contains(dirty, "500 MB") {
		t.Errorf("dirty row = %q", dirty)
	}
}

func TestWorktreesSizesNotMeasuredYetShowAnEllipsis(t *testing.T) {
	out := screen(diskModel().open().m)
	pending := rowOf(t, out, "feat-d")
	if !strings.Contains(pending, "…") || strings.Contains(pending, " GB") || strings.Contains(pending, " MB") {
		t.Errorf("pending row = %q, want … in place of a size", pending)
	}
}

func TestWorktreesReclaimableIsTheSumOfWhatCleanupWouldRemoveOrBackUp(t *testing.T) {
	rows := diskRows()
	rows[3].Size = 3 * gb
	out := screen(diskModel(diskView(rows...)).open().m)
	if !strings.Contains(out, "reclaimable 2.5 GB") || strings.Contains(out, "reclaimable 2.5 GB+") {
		t.Errorf("header:\n%s\nwant reclaimable 2.5 GB, complete: the kept rows do not count", out)
	}
	if !strings.Contains(out, "worktrees 7.0 GB ") {
		t.Errorf("header lacks the measured worktree total:\n%s", out)
	}

	rows = diskRows()
	rows[0].Size = domain.SizePending
	out = screen(diskModel(diskView(rows...)).open().m)
	if !strings.Contains(out, "reclaimable 500 MB+") {
		t.Errorf("with a removable size pending the header should say 500 MB+:\n%s", out)
	}
}

func TestWorktreesOnlyRowsNeedingADecisionShowInlineActions(t *testing.T) {
	out := screen(diskModel().open().m)
	if n := strings.Count(out, "b backup + remove"); n != 1 {
		t.Errorf("%d inline action lines, want exactly the one for the dirty merged worktree:\n%s", n, out)
	}
	inline := lineWith(t, out, "b backup + remove")
	if !strings.Contains(inline, "2 uncommitted changes") {
		t.Errorf("inline actions = %q, want the reason", inline)
	}
}

func TestWorktreesRecentlyCleanedComesFromTheAuditLog(t *testing.T) {
	out := screen(diskModel().open().m)
	if !strings.Contains(out, "RECENTLY CLEANED") {
		t.Fatalf("no recently cleaned section:\n%s", out)
	}
	if line := lineWith(t, out, "old-merged"); !strings.Contains(line, "12:03") || !strings.Contains(line, "removed") {
		t.Errorf("first entry = %q", line)
	}
	if line := lineWith(t, out, "old-dirty"); !strings.Contains(line, "11:40") || !strings.Contains(line, "backed up") {
		t.Errorf("second entry = %q", line)
	}
	empty := diskView(diskRows()...)
	empty.Recent = nil
	if out := screen(diskModel(empty).open().m); !strings.Contains(out, "nothing yet") {
		t.Errorf("empty history:\n%s", out)
	}
}

func TestWorktreesRemoveAsksThenCallsTheEngineAndTheRowGoesOnTheNextFetch(t *testing.T) {
	after := diskView(diskRows()[1:]...)
	r := diskModel(diskView(diskRows()...), after).open()
	r.press("d")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "remove api:part-1? y/n") {
		t.Errorf("status = %q, want a confirmation", got)
	}
	if len(r.disk.actions) != 0 {
		t.Fatal("asked the engine before the user confirmed")
	}
	r.press("y")
	if !reflect.DeepEqual(r.disk.actions, []diskAction{{"/wt/w01-0", false}}) {
		t.Errorf("actions = %+v", r.disk.actions)
	}
	out := screen(r.m)
	if strings.Contains(out, "feat-a") {
		t.Errorf("the removed worktree is still listed:\n%s", out)
	}
	if r.disk.fetched() != 2 {
		t.Errorf("%d fetches, want the view refetched once after the action", r.disk.fetched())
	}
	if !strings.Contains(lastLine(out), "removed") {
		t.Errorf("status = %q, want the outcome", lastLine(out))
	}
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

func TestWorktreesRemoveDeclinedDoesNothing(t *testing.T) {
	r := diskModel().open().press("d", "n")
	if len(r.disk.actions) != 0 || r.disk.fetched() != 1 {
		t.Errorf("actions %+v, fetches %d", r.disk.actions, r.disk.fetched())
	}
}

func TestWorktreesRemoveRefusesWhatIsNotAPlainRemove(t *testing.T) {
	r := diskModel().open().press("j", "d")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "press b") {
		t.Errorf("on a dirty worktree the status = %q, want it to point at b", got)
	}
	r = diskModel().open().press("j", "j", "d")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "its session is live") {
		t.Errorf("on a kept worktree the status = %q, want the reason", got)
	}
	if len(r.disk.actions) != 0 {
		t.Errorf("actions = %+v; nothing should reach the engine", r.disk.actions)
	}
}

func TestWorktreesBackupAndRemoveNeedsADirtyOrDetachedRowAndConfirms(t *testing.T) {
	r := diskModel().open().press("j", "b")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "back up and remove web:part-2? y/n") {
		t.Errorf("status = %q", got)
	}
	r.press("y")
	if !reflect.DeepEqual(r.disk.actions, []diskAction{{"/wt/w01-1", true}}) {
		t.Errorf("actions = %+v", r.disk.actions)
	}

	r = diskModel().open().press("j", "j", "b")
	if len(r.disk.actions) != 0 {
		t.Errorf("b on a kept worktree reached the engine: %+v", r.disk.actions)
	}
}

func TestWorktreesShowWhyTheEngineKeptAWorktree(t *testing.T) {
	r := diskModel().open()
	r.disk.item = rpc.CleanupItem{Outcome: "kept: in use by nvim (pid 7)"}
	r.press("d", "y")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "kept: in use by nvim (pid 7)") {
		t.Errorf("status = %q", got)
	}
}

func TestWorktreesActionErrorsAreShown(t *testing.T) {
	r := diskModel().open()
	r.disk.err = errors.New("failed: daemon busy")
	r.press("d", "y")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "daemon busy") {
		t.Errorf("status = %q", got)
	}
}

func TestWorktreesKillPortAsksThenKillsTheGroupsOfTheSelectedRow(t *testing.T) {
	r := diskModel().open().press("j", "j", "k")
	if got := lastLine(screen(r.m)); !strings.Contains(got, "kill :3000? y/n") {
		t.Errorf("status = %q", got)
	}
	r.press("y")
	if !reflect.DeepEqual(r.killer.killed, [][]int{{200}}) {
		t.Errorf("killed = %v", r.killer.killed)
	}

	r = diskModel().open().press("k", "y")
	if len(r.killer.killed) != 0 {
		t.Errorf("k on a row without ports killed %v", r.killer.killed)
	}
}

func TestWorktreesGoToSessionFocusesItAndClosesTheView(t *testing.T) {
	r := diskModel().open().press("j", "j", "g")
	if !reflect.DeepEqual(r.attender.focused, []string{"s02"}) || r.focuser.calls != 1 {
		t.Errorf("focused %v, focus calls %d", r.attender.focused, r.focuser.calls)
	}
	if !reflect.DeepEqual(r.disk.layouts, []bool{true, false}) || r.m.Selected() != "s02" {
		t.Errorf("layouts %v, selected %q; want the view closed and s02 selected", r.disk.layouts, r.m.Selected())
	}
	if out := screen(r.m); strings.Contains(out, "WORKTREES & DISK") {
		t.Errorf("view still open:\n%s", out)
	}
}

func TestWorktreesShellHereOpensAShellInTheWorktree(t *testing.T) {
	r := diskModel().open().press("j", "o")
	if !reflect.DeepEqual(r.disk.shells, []string{"w01-1"}) || !reflect.DeepEqual(r.disk.layouts, []bool{true, false}) {
		t.Errorf("shells %v, layouts %v", r.disk.shells, r.disk.layouts)
	}
}

func TestWorktreesEscClosesAndPutsTheSidebarBack(t *testing.T) {
	r := diskModel().open()
	r.m = drive(r.m, keyEsc)
	if !reflect.DeepEqual(r.disk.layouts, []bool{true, false}) {
		t.Errorf("layouts = %v", r.disk.layouts)
	}
	if out := screen(r.m); !strings.Contains(out, "SESSIONS") {
		t.Errorf("sidebar not back:\n%s", out)
	}
}

func TestWorktreesRemovedWorktreeLeavesTheTableAtOnce(t *testing.T) {
	r := diskModel().open()
	r.m = drive(r.m, tui.DiffMsg(rpc.Diff{Seq: 9, RemovedWorktree: "w01-1"}))
	out := screen(r.m)
	if strings.Contains(out, "feat-b") || !strings.Contains(out, "feat-a") {
		t.Errorf("table after the diff:\n%s", out)
	}
	if r.disk.fetched() != 1 {
		t.Errorf("%d fetches, want no refetch for a removal", r.disk.fetched())
	}
}

func TestWorktreesRemovedByCleanupLeaveTheSidebarToo(t *testing.T) {
	st := fixture(1, 2)
	m := newModel(&st, nil)
	if out := screen(m); !strings.Contains(out, "2 worktrees") {
		t.Fatalf("fixture sidebar does not count both worktrees:\n%s", out)
	}
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 3, RemovedWorktree: "w01-1"}))
	if out := screen(m); !strings.Contains(out, "1 worktree ") {
		t.Errorf("sidebar after the removal:\n%s", out)
	}
}

func TestWorktreesRefetchWhileSizesArePendingAndSlowlyOtherwise(t *testing.T) {
	r := diskModel().open()
	for range 9 {
		r.m = drive(r.m, tui.TickMsg{})
	}
	if r.disk.fetched() != 1 {
		t.Fatalf("%d fetches before two seconds, want 1", r.disk.fetched())
	}
	r.m = drive(r.m, tui.TickMsg{})
	if r.disk.fetched() != 2 {
		t.Fatalf("%d fetches with a size pending after two seconds, want 2", r.disk.fetched())
	}

	done := diskRows()
	done[3].Size = 1 * gb
	r = diskModel(diskView(done...)).open()
	for range 49 {
		r.m = drive(r.m, tui.TickMsg{})
	}
	if r.disk.fetched() != 1 {
		t.Fatalf("%d fetches with every size known before ten seconds, want 1", r.disk.fetched())
	}
	r.m = drive(r.m, tui.TickMsg{})
	if r.disk.fetched() != 2 {
		t.Fatalf("%d fetches after ten seconds, want 2", r.disk.fetched())
	}
}

func TestWorktreesDoNotRefetchWhenClosed(t *testing.T) {
	r := diskModel()
	for range 100 {
		r.m = drive(r.m, tui.TickMsg{})
	}
	if r.disk.fetched() != 0 {
		t.Errorf("%d fetches with the view closed", r.disk.fetched())
	}
}

func TestWorktreesDaemonErrorIsShownInsteadOfTheTable(t *testing.T) {
	r := diskModel()
	r.disk.err = errors.New("unavailable: the disk view is not configured")
	r.open()
	if out := screen(r.m); !strings.Contains(out, "the disk view is not configured") {
		t.Errorf("view:\n%s", out)
	}
}

func TestWorktreesKeyIsOffWithoutADiskClient(t *testing.T) {
	st := fixture(1, 1)
	m := newModel(&st, nil)
	m = drive(m, key("w"))
	if strings.Contains(screen(m), "WORKTREES & DISK") {
		t.Error("w opened the view with no daemon client")
	}
}

func TestGoldenWorktrees(t *testing.T) {
	r := diskModel().open()
	r.m = update(r.m, tea.WindowSizeMsg{Width: 120, Height: 30})
	golden.RequireEqual(t, screen(r.m))
}

func TestWorktreesDiskBarStaysItsWidthWhenWorktreesShareFiles(t *testing.T) {
	rows := []domain.DiskRow{
		{WorktreeID: "w01-0", Size: 40 * gb, Action: domain.CleanupKeep},
		{WorktreeID: "w01-1", Size: 40 * gb, Action: domain.CleanupKeep},
		{WorktreeID: "w02-0", Size: 40 * gb, Action: domain.CleanupRemove},
	}
	v := diskView(rows...)
	v.Total, v.Free = 100*gb, 60*gb
	out := screen(diskModel(v).open().m)
	for _, line := range strings.Split(out, "\n") {
		if strings.ContainsAny(line, "█░") {
			cells := strings.Count(line, "█") + strings.Count(line, "░")
			if cells != 40 || strings.Count(line, "░") != 24 {
				t.Fatalf("bar has %d cells, %d free; want 40 with 60%% free:\n%s", cells, strings.Count(line, "░"), line)
			}
			return
		}
	}
	t.Fatalf("no disk bar:\n%s", out)
}
