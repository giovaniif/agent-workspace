package daemon

import (
	"context"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

const titleEvery = 250 * time.Millisecond

type titleInput struct {
	session domain.Session
	cwd     string
}

func (d *Daemon) paneTitles(ctx context.Context) {
	if d.hs.host == nil {
		return
	}
	set := map[app.PaneID]string{}
	var seen uint64
	stale := true
	tick, stop := ticker(titleEvery)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
		var inputs []titleInput
		var wts []domain.Worktree
		var projects []domain.Project
		var shells []domain.ShellTab
		changed := false
		ok := d.query(func(s *state) {
			if !stale && s.seq == seen {
				return
			}
			seen, changed, stale = s.seq, true, false
			for _, x := range s.sessions {
				if x.Pane != "" {
					x.WorktreeIDs = slices.Clone(x.WorktreeIDs)
					inputs = append(inputs, titleInput{session: x, cwd: s.hints.cwd[x.ID]})
				}
			}
			wts = make([]domain.Worktree, 0, len(s.worktrees))
			for _, w := range s.worktrees {
				if w.PR != nil {
					pr := *w.PR
					w.PR = &pr
				}
				wts = append(wts, w)
			}
			projects = sorted(s.projects)
			shells = sorted(s.shellTabs)
		})
		if !ok || !changed {
			continue
		}
		slices.SortFunc(wts, func(a, b domain.Worktree) int { return strings.Compare(a.ID, b.ID) })
		want := make(map[app.PaneID]string, len(inputs))
		live := make([]domain.Session, 0, len(inputs))
		for _, in := range inputs {
			live = append(live, in.session)
		}
		for _, in := range inputs {
			title := domain.AgentTitle(in.session, wts, in.cwd)
			if home, ok := domain.TabHome(in.session, wts, projects); ok {
				title = domain.TabbedTitle(domain.TabStrip(domain.WorktreeTabs(home, live, shells), in.session.ID), title)
			}
			want[app.PaneID(in.session.Pane)] = title
		}
		for _, sh := range shells {
			strip := domain.TabStrip(domain.WorktreeTabs(sh.Worktree, live, shells), sh.ID)
			want[app.PaneID(sh.Pane)] = domain.TabbedTitle(strip, "shell · "+path.Base(sh.Worktree))
		}
		for pane, title := range want {
			if set[pane] == title {
				continue
			}
			if d.hs.host.SetTitle(ctx, pane, title) != nil {
				stale = true
				continue
			}
			set[pane] = title
		}
		for pane := range set {
			if _, live := want[pane]; !live {
				delete(set, pane)
			}
		}
	}
}
