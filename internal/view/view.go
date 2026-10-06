package view

import (
	"slices"
	"sort"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type Session struct {
	domain.Session
	Name   string     `json:"name"`
	Where  string     `json:"where"`
	Banner string     `json:"banner"`
	Since  *time.Time `json:"since"`
}

type Quota struct {
	domain.Quota
	Label   string    `json:"label"`
	Low     bool      `json:"low"`
	StaleAt time.Time `json:"stale_at"`
}

type State struct {
	Seq        uint64              `json:"seq"`
	Workspaces []domain.Workspace  `json:"workspaces"`
	Tasks      []domain.Task       `json:"tasks"`
	Worktrees  []domain.Worktree   `json:"worktrees"`
	Sessions   []Session           `json:"sessions"`
	Limits     []Quota             `json:"limits"`
	Queue      []domain.LaunchItem `json:"queue"`
	Sends      []domain.QueuedSend `json:"sends"`
}

type Diff struct {
	Seq              uint64               `json:"seq"`
	RemovedWorkspace string               `json:"removed_workspace,omitempty"`
	RemovedWorktree  string               `json:"removed_worktree,omitempty"`
	RemovedSession   string               `json:"removed_session,omitempty"`
	Workspace        *domain.Workspace    `json:"workspace,omitempty"`
	Task             *domain.Task         `json:"task,omitempty"`
	Worktree         *domain.Worktree     `json:"worktree,omitempty"`
	Session          *Session             `json:"session,omitempty"`
	Limits           *[]Quota             `json:"limits,omitempty"`
	Queue            *[]domain.LaunchItem `json:"queue,omitempty"`
	Sends            *[]domain.QueuedSend `json:"sends,omitempty"`
}

type derived struct {
	name   string
	where  string
	banner string
	since  time.Time
}

type View struct {
	tasks     map[string]domain.Task
	worktrees map[string]domain.Worktree
	sessions  map[string]domain.Session
	events    map[string][]domain.SessionEvent
	shown     map[string]derived
	limits    []Quota
}

func New(st rpc.State) (*View, *State) {
	v := &View{
		tasks:     map[string]domain.Task{},
		worktrees: map[string]domain.Worktree{},
		sessions:  map[string]domain.Session{},
		events:    map[string][]domain.SessionEvent{},
		shown:     map[string]derived{},
	}
	for _, t := range st.Tasks {
		v.tasks[t.ID] = t
	}
	worktrees := make([]domain.Worktree, 0, len(st.Worktrees))
	for _, wt := range st.Worktrees {
		v.worktrees[wt.ID] = wt
		worktrees = append(worktrees, withoutPorts(wt))
	}
	for _, ev := range st.Events {
		v.addEvent(ev)
	}
	for _, s := range st.Sessions {
		v.sessions[s.ID] = s
	}
	sessions := make([]Session, 0, len(st.Sessions))
	for _, s := range st.Sessions {
		sessions = append(sessions, v.show(s))
	}
	v.limits = v.quotas()
	return v, &State{
		Seq:        st.Seq,
		Workspaces: orEmpty(st.Workspaces),
		Tasks:      orEmpty(st.Tasks),
		Worktrees:  worktrees,
		Sessions:   sessions,
		Limits:     v.limits,
		Queue:      orEmpty(st.Queue),
		Sends:      orEmpty(st.Sends),
	}
}

func (v *View) Apply(d rpc.Diff) []*Diff {
	v.remember(d)
	out := &Diff{
		Seq:              d.Seq,
		RemovedWorkspace: d.RemovedWorkspace,
		RemovedWorktree:  d.RemovedWorktree,
		RemovedSession:   d.RemovedSession,
		Workspace:        d.Workspace,
		Task:             d.Task,
		Queue:            d.Queue,
		Sends:            d.Sends,
	}
	if d.Worktree != nil {
		wt := withoutPorts(*d.Worktree)
		out.Worktree = &wt
	}
	if d.Session != nil {
		s := v.show(*d.Session)
		out.Session = &s
	}
	if d.Session != nil || d.RemovedSession != "" {
		if limits := v.quotas(); !slices.Equal(limits, v.limits) {
			v.limits = limits
			out.Limits = &limits
		}
	}
	var diffs []*Diff
	if out.keep() {
		diffs = append(diffs, out)
	}
	return append(diffs, v.changedSessions(d)...)
}

func (v *View) remember(d rpc.Diff) {
	if d.Task != nil {
		v.tasks[d.Task.ID] = *d.Task
	}
	if d.Worktree != nil {
		v.worktrees[d.Worktree.ID] = *d.Worktree
	}
	if d.RemovedWorktree != "" {
		delete(v.worktrees, d.RemovedWorktree)
	}
	if d.Event != nil {
		v.addEvent(*d.Event)
	}
	if d.Session != nil {
		v.sessions[d.Session.ID] = *d.Session
	}
	if d.RemovedSession != "" {
		delete(v.sessions, d.RemovedSession)
		delete(v.events, d.RemovedSession)
		delete(v.shown, d.RemovedSession)
	}
}

func (v *View) changedSessions(d rpc.Diff) []*Diff {
	ids := make([]string, 0, len(v.sessions))
	for id := range v.sessions {
		if d.Session == nil || d.Session.ID != id {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []*Diff
	for _, id := range ids {
		s := v.sessions[id]
		if v.derive(s) == v.shown[id] {
			continue
		}
		shown := v.show(s)
		out = append(out, &Diff{Seq: d.Seq, Session: &shown})
	}
	return out
}

func (v *View) addEvent(ev domain.SessionEvent) {
	kept := append(v.events[ev.SessionID], ev)
	if len(kept) > domain.SessionEventsKept {
		kept = kept[len(kept)-domain.SessionEventsKept:]
	}
	v.events[ev.SessionID] = kept
}

func (v *View) show(s domain.Session) Session {
	d := v.derive(s)
	v.shown[s.ID] = d
	out := Session{Session: s, Name: d.name, Where: d.where, Banner: d.banner}
	if !d.since.IsZero() {
		since := d.since
		out.Since = &since
	}
	return out
}

func (v *View) derive(s domain.Session) derived {
	var worktrees []domain.Worktree
	var prs []domain.PullRequest
	for _, id := range s.WorktreeIDs {
		wt, ok := v.worktrees[id]
		if !ok {
			continue
		}
		worktrees = append(worktrees, wt)
		if wt.PR != nil {
			prs = append(prs, *wt.PR)
		}
	}
	events := v.events[s.ID]
	since, _ := domain.StateSince(s, events)
	unmuted := s.SetMuted(false)
	banner, _ := domain.BannerFor(domain.BannerInput{
		Session: unmuted,
		Effect:  domain.Effect{Kind: domain.EffectNotify, State: s.State},
		Events:  events,
		Now:     since,
	})
	return derived{
		name:   domain.NameFor(v.tasks[s.TaskID], prs),
		where:  domain.WorktreeLabel(worktrees),
		banner: banner.Body,
		since:  since,
	}
}

func (v *View) quotas() []Quota {
	ids := make([]string, 0, len(v.sessions))
	for id := range v.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sessions := make([]domain.Session, 0, len(ids))
	for _, id := range ids {
		sessions = append(sessions, v.sessions[id])
	}
	quotas := domain.Quotas(sessions)
	out := make([]Quota, 0, len(quotas))
	for _, q := range quotas {
		out = append(out, Quota{Quota: q, Label: domain.WindowLabel(q.Window), Low: q.Low(), StaleAt: q.ReportedAt.Add(domain.StaleQuotaAfter)})
	}
	return out
}

func (d *Diff) keep() bool {
	return d.RemovedWorkspace != "" || d.RemovedWorktree != "" || d.RemovedSession != "" ||
		d.Workspace != nil || d.Task != nil || d.Worktree != nil || d.Session != nil || d.Queue != nil || d.Sends != nil || d.Limits != nil
}

func withoutPorts(wt domain.Worktree) domain.Worktree {
	wt.Ports = nil
	return wt
}

func orEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
