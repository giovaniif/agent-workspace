package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	diskWorkers = 2
	diskSizeTTL = 5 * time.Minute
)

const recentCleanups = 8

type DiskDeps struct {
	Sizes      *app.DiskSizes
	Volume     app.VolumeStat
	History    app.CleanupHistory
	VolumePath string
	DepsStore  string
}

func WithDisk(deps DiskDeps) Option {
	return func(d *Daemon) { d.disk = deps }
}

func (d *Daemon) diskView(req rpc.Request) (*rpc.Response, bool) {
	if d.disk.Sizes == nil || d.cl.c == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "the disk view is not configured"), true
	}
	results, ok := d.runCleanup(d.ws.ctx, false)
	if !ok {
		return nil, false
	}
	view := rpc.DiskView{AutoCleanEvery: d.cl.every, Rows: make([]domain.DiskRow, 0, len(results)), Recent: []rpc.RecentCleanup{}}
	for _, r := range results {
		w := r.Decision.Worktree
		size, known := d.disk.Sizes.Get(w.Path)
		if !known {
			size = domain.SizePending
		}
		view.Rows = append(view.Rows, domain.DiskRow{WorktreeID: w.ID, Size: size, Action: r.Decision.Action, Reason: r.Decision.Reason})
	}
	view.Reclaimable, view.ReclaimablePending = domain.Reclaimable(view.Rows)
	view.WorktreesSize, view.WorktreesPending = domain.TotalSize(view.Rows)
	d.publishReclaimable(rpc.Reclaimable{Size: view.Reclaimable, Pending: view.ReclaimablePending})
	if next := d.cl.nextRun(); !next.IsZero() {
		view.NextCleanup = &next
	}
	if d.disk.Volume != nil {
		view.Free, view.Total, _ = d.disk.Volume.Stat(d.disk.VolumePath)
	}
	if d.disk.DepsStore != "" {
		size, known := d.disk.Sizes.Get(d.disk.DepsStore)
		if !known {
			size = domain.SizePending
		}
		view.DepsStore = &rpc.DepsStore{Path: d.disk.DepsStore, Size: size}
	}
	if d.disk.History != nil {
		for _, r := range d.disk.History.Recent(recentCleanups) {
			view.Recent = append(view.Recent, rpc.RecentCleanup{At: r.At, Path: r.Path, Branch: r.Branch, Action: r.Action, Outcome: r.Outcome})
		}
	}
	return result(req.ID, view), true
}

type ReclaimableChanged struct{ Reclaimable rpc.Reclaimable }

func (e ReclaimableChanged) apply(s *state) rpc.Diff {
	s.reclaimable = e.Reclaimable
	r := e.Reclaimable
	return rpc.Diff{Reclaimable: &r}
}

func (d *Daemon) publishReclaimable(r rpc.Reclaimable) {
	d.query(func(s *state) {
		if s.reclaimable != r {
			s.emit(ReclaimableChanged{Reclaimable: r})
		}
	})
}

func (d *Daemon) cleanupWorktree(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.CleanupWorktreeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "cleanup.worktree params: "+err.Error()), true
	}
	if d.cl.c == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "cleanup is not configured"), true
	}
	d.cl.mu.Lock()
	defer d.cl.mu.Unlock()
	wts, activity, ok := d.cleanupInputs()
	if !ok {
		return nil, false
	}
	for _, w := range wts {
		if w.Path != p.Path {
			continue
		}
		res := d.cl.c.RemoveWorktree(d.ws.ctx, w, activity, p.Backup)
		if res.Outcome == "removed" || strings.HasSuffix(res.Outcome, ", removed") {
			d.afterRemoval(w)
		}
		dec := res.Decision
		return result(req.ID, rpc.CleanupItem{Path: w.Path, Branch: w.Branch, Action: dec.Action, Reason: dec.Reason, Outcome: res.Outcome}), true
	}
	return errorResponse(req.ID, rpc.CodeNotFound, "no worktree "+p.Path), true
}

func (d *Daemon) afterRemoval(w domain.Worktree) {
	if d.disk.Sizes != nil {
		d.disk.Sizes.Forget(w.Path)
	}
	d.Post(WorktreeRemoved{ID: w.ID})
	d.st.hints.wake()
}

func DepsStorePath() string {
	if p := os.Getenv("AGENTWS_DEPS_STORE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, p := range []string{
		filepath.Join(home, "Library", "pnpm", "store"),
		filepath.Join(home, ".local", "share", "pnpm", "store"),
	} {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}
