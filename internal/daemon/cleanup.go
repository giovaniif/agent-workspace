package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const DefaultCleanupEvery = 10 * time.Minute

type cleanupWorker struct {
	c     *app.Cleanup
	every time.Duration
	kick  chan struct{}
	mu    sync.Mutex
	next  atomic.Int64
}

func (w *cleanupWorker) schedule() {
	if w.c != nil && w.every > 0 {
		w.next.Store(time.Now().Add(w.every).UnixNano())
	}
}

func (w *cleanupWorker) nextRun() time.Time {
	n := w.next.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func WithCleanup(c *app.Cleanup, every time.Duration) Option {
	return func(d *Daemon) { d.cl.c, d.cl.every = c, every }
}

func (w *cleanupWorker) wake() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (d *Daemon) cleanupEvery(ctx context.Context) {
	if d.cl.c == nil {
		return
	}
	tick, stop := ticker(d.cl.every)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			d.cl.schedule()
			d.runCleanup(ctx, true)
		case <-d.cl.kick:
			d.runCleanup(ctx, true)
		}
	}
}

func (d *Daemon) cleanupInputs() ([]domain.Worktree, func(domain.Worktree) app.SessionActivity, bool) {
	var wts []domain.Worktree
	activity := map[string]app.SessionActivity{}
	ok := d.query(func(s *state) {
		wts = sorted(s.worktrees)
		projects := sorted(s.projects)
		for _, w := range wts {
			var a app.SessionActivity
			if p, found := domain.ProjectOfWorktree(projects, w); found {
				a.Project = p.Name
			}
			if x, found := s.sessions[w.SessionID]; found && w.SessionID != "" {
				a.Live = x.State != domain.StateIdle
				if evs := s.events[x.ID]; len(evs) > 0 {
					a.LastActivity = evs[len(evs)-1].At
				}
			}
			activity[w.ID] = a
		}
	})
	return wts, func(w domain.Worktree) app.SessionActivity { return activity[w.ID] }, ok
}

func (d *Daemon) runCleanup(ctx context.Context, execute bool) ([]app.CleanupResult, bool) {
	d.cl.mu.Lock()
	defer d.cl.mu.Unlock()
	wts, activity, ok := d.cleanupInputs()
	if !ok {
		return nil, false
	}
	if !execute {
		var out []app.CleanupResult
		for _, dec := range d.cl.c.Plan(ctx, wts, activity) {
			out = append(out, app.CleanupResult{Decision: dec})
		}
		return out, true
	}
	results := d.cl.c.Execute(ctx, wts, activity)
	for _, r := range results {
		if r.Outcome == "removed" {
			d.st.hints.wake()
			break
		}
	}
	return results, true
}

func (d *Daemon) cleanupMethod(req rpc.Request) (*rpc.Response, bool) {
	if d.cl.c == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "cleanup is not configured"), true
	}
	results, ok := d.runCleanup(d.ws.ctx, req.Method == rpc.MethodCleanupRun)
	items := make([]rpc.CleanupItem, 0, len(results))
	for _, r := range results {
		w := r.Decision.Worktree
		items = append(items, rpc.CleanupItem{Path: w.Path, Branch: w.Branch, Action: r.Decision.Action, Reason: r.Decision.Reason, Outcome: r.Outcome})
	}
	return result(req.ID, items), ok
}
