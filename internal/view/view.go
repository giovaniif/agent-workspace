package view

import (
	"reflect"
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

type FailingCheck struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type BoardPR struct {
	Number            int            `json:"number"`
	Title             string         `json:"title"`
	URL               string         `json:"url"`
	Branch            string         `json:"branch"`
	State             string         `json:"state"`
	Checks            string         `json:"checks"`
	FailingChecks     []FailingCheck `json:"failing_checks"`
	ReviewDecision    string         `json:"review_decision"`
	UnresolvedThreads int            `json:"unresolved_threads"`
	BotComments       int            `json:"bot_comments_since_push"`
	Mergeable         string         `json:"mergeable"`
	ReadyToMerge      bool           `json:"ready_to_merge"`
	Blockers          []string       `json:"blockers"`
}

type NativeSession struct {
	Session
	Order int       `json:"order"`
	Board []BoardPR `json:"board"`
}

type NativeState struct {
	Seq         uint64                `json:"seq"`
	Workspaces  []domain.Workspace    `json:"workspaces"`
	Tasks       []domain.Task         `json:"tasks"`
	Worktrees   []domain.Worktree     `json:"worktrees"`
	Sessions    []NativeSession       `json:"sessions"`
	Limits      []Quota               `json:"limits"`
	Queue       []domain.LaunchItem   `json:"queue"`
	Sends       []domain.QueuedSend   `json:"sends"`
	Events      []domain.SessionEvent `json:"events"`
	Subagents   []domain.Subagent     `json:"subagents"`
	Drafts      []domain.ReviewDraft  `json:"drafts"`
	Reclaimable rpc.Reclaimable       `json:"reclaimable"`
}

type NativeDiff struct {
	Seq              uint64                `json:"seq"`
	RemovedWorkspace string                `json:"removed_workspace,omitempty"`
	RemovedWorktree  string                `json:"removed_worktree,omitempty"`
	RemovedSession   string                `json:"removed_session,omitempty"`
	Workspace        *domain.Workspace     `json:"workspace,omitempty"`
	Task             *domain.Task          `json:"task,omitempty"`
	Worktree         *domain.Worktree      `json:"worktree,omitempty"`
	Session          *NativeSession        `json:"session,omitempty"`
	Limits           *[]Quota              `json:"limits,omitempty"`
	Queue            *[]domain.LaunchItem  `json:"queue,omitempty"`
	Sends            *[]domain.QueuedSend  `json:"sends,omitempty"`
	Event            *domain.SessionEvent  `json:"event,omitempty"`
	Subagent         *domain.Subagent      `json:"subagent,omitempty"`
	Draft            *domain.ReviewDraft   `json:"draft,omitempty"`
	Comment          *domain.ReviewComment `json:"comment,omitempty"`
	Reclaimable      *rpc.Reclaimable      `json:"reclaimable,omitempty"`
}

type derived struct {
	name   string
	where  string
	banner string
	since  time.Time
	order  int
	board  []BoardPR
}

type View struct {
	native    bool
	tasks     map[string]domain.Task
	worktrees map[string]domain.Worktree
	sessions  map[string]domain.Session
	order     []string
	events    map[string][]domain.SessionEvent
	shown     map[string]derived
	limits    []Quota
}

func New(st rpc.State) (*View, *State) {
	v, native := build(st, false)
	worktrees := make([]domain.Worktree, 0, len(native.Worktrees))
	for _, wt := range native.Worktrees {
		worktrees = append(worktrees, withoutPorts(wt))
	}
	sessions := make([]Session, 0, len(native.Sessions))
	for _, s := range native.Sessions {
		sessions = append(sessions, s.Session)
	}
	return v, &State{
		Seq:        native.Seq,
		Workspaces: native.Workspaces,
		Tasks:      native.Tasks,
		Worktrees:  worktrees,
		Sessions:   sessions,
		Limits:     native.Limits,
		Queue:      native.Queue,
		Sends:      native.Sends,
	}
}

func NewNative(st rpc.State) (*View, *NativeState) {
	return build(st, true)
}

func build(st rpc.State, native bool) (*View, *NativeState) {
	v := &View{
		native:    native,
		tasks:     map[string]domain.Task{},
		worktrees: map[string]domain.Worktree{},
		sessions:  map[string]domain.Session{},
		events:    map[string][]domain.SessionEvent{},
		shown:     map[string]derived{},
	}
	for _, t := range st.Tasks {
		v.tasks[t.ID] = t
	}
	for _, wt := range st.Worktrees {
		v.worktrees[wt.ID] = wt
	}
	for _, ev := range st.Events {
		v.addEvent(ev)
	}
	for _, s := range st.Sessions {
		v.addSession(s)
	}
	orders := v.orders()
	sessions := make([]NativeSession, 0, len(st.Sessions))
	for _, s := range st.Sessions {
		sessions = append(sessions, v.show(s, orders))
	}
	v.limits = v.quotas()
	return v, &NativeState{
		Seq:         st.Seq,
		Workspaces:  orEmpty(st.Workspaces),
		Tasks:       orEmpty(st.Tasks),
		Worktrees:   orEmpty(st.Worktrees),
		Sessions:    sessions,
		Limits:      v.limits,
		Queue:       orEmpty(st.Queue),
		Sends:       orEmpty(st.Sends),
		Events:      orEmpty(st.Events),
		Subagents:   orEmpty(st.Subagents),
		Drafts:      orEmpty(st.Drafts),
		Reclaimable: st.Reclaimable,
	}
}

func (v *View) Apply(d rpc.Diff) []*Diff {
	var out []*Diff
	for _, n := range v.ApplyNative(d) {
		if p := n.phone(); p.keep() {
			out = append(out, p)
		}
	}
	return out
}

func (v *View) ApplyNative(d rpc.Diff) []*NativeDiff {
	v.remember(d)
	orders := v.orders()
	out := &NativeDiff{
		Seq:              d.Seq,
		RemovedWorkspace: d.RemovedWorkspace,
		RemovedWorktree:  d.RemovedWorktree,
		RemovedSession:   d.RemovedSession,
		Workspace:        d.Workspace,
		Task:             d.Task,
		Worktree:         d.Worktree,
		Queue:            d.Queue,
		Sends:            d.Sends,
		Event:            d.Event,
		Subagent:         d.Subagent,
		Draft:            d.Draft,
		Comment:          d.Comment,
		Reclaimable:      d.Reclaimable,
	}
	if d.Session != nil {
		s := v.show(*d.Session, orders)
		out.Session = &s
	}
	if d.Session != nil || d.RemovedSession != "" {
		if limits := v.quotas(); !slices.Equal(limits, v.limits) {
			v.limits = limits
			out.Limits = &limits
		}
	}
	var diffs []*NativeDiff
	if out.keep() {
		diffs = append(diffs, out)
	}
	return append(diffs, v.changedSessions(d, orders)...)
}

func (d *NativeDiff) phone() *Diff {
	out := &Diff{
		Seq:              d.Seq,
		RemovedWorkspace: d.RemovedWorkspace,
		RemovedWorktree:  d.RemovedWorktree,
		RemovedSession:   d.RemovedSession,
		Workspace:        d.Workspace,
		Task:             d.Task,
		Limits:           d.Limits,
		Queue:            d.Queue,
		Sends:            d.Sends,
	}
	if d.Worktree != nil {
		wt := withoutPorts(*d.Worktree)
		out.Worktree = &wt
	}
	if d.Session != nil {
		out.Session = &d.Session.Session
	}
	return out
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
		v.addSession(*d.Session)
	}
	if d.RemovedSession != "" {
		delete(v.sessions, d.RemovedSession)
		delete(v.events, d.RemovedSession)
		delete(v.shown, d.RemovedSession)
		v.order = slices.DeleteFunc(v.order, func(id string) bool { return id == d.RemovedSession })
	}
}

func (v *View) addSession(s domain.Session) {
	if _, ok := v.sessions[s.ID]; !ok {
		v.order = append(v.order, s.ID)
	}
	v.sessions[s.ID] = s
}

func (v *View) changedSessions(d rpc.Diff, orders map[string]int) []*NativeDiff {
	ids := make([]string, 0, len(v.sessions))
	for id := range v.sessions {
		if d.Session == nil || d.Session.ID != id {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []*NativeDiff
	for _, id := range ids {
		s := v.sessions[id]
		if reflect.DeepEqual(v.derive(s, orders), v.shown[id]) {
			continue
		}
		shown := v.show(s, orders)
		out = append(out, &NativeDiff{Seq: d.Seq, Session: &shown})
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

func (v *View) orders() map[string]int {
	if !v.native {
		return nil
	}
	flat := make([]domain.Session, 0, len(v.order))
	for _, id := range v.order {
		s := v.sessions[id]
		s.TaskID = ""
		flat = append(flat, s)
	}
	orders := map[string]int{}
	for _, g := range domain.Sidebar(nil, flat) {
		for i, s := range g.Sessions {
			orders[s.ID] = i
		}
	}
	return orders
}

func (v *View) show(s domain.Session, orders map[string]int) NativeSession {
	d := v.derive(s, orders)
	v.shown[s.ID] = d
	out := NativeSession{Session: Session{Session: s, Name: d.name, Where: d.where, Banner: d.banner}, Order: d.order, Board: d.board}
	if !d.since.IsZero() {
		since := d.since
		out.Since = &since
	}
	return out
}

func (v *View) derive(s domain.Session, orders map[string]int) derived {
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
	out := derived{
		name:   domain.NameFor(v.tasks[s.TaskID], prs),
		where:  domain.WorktreeLabel(worktrees),
		banner: banner.Body,
		since:  since,
	}
	if v.native {
		out.order = -1
		if i, ok := orders[s.ID]; ok {
			out.order = i
		}
		out.board = board(domain.BuildSessionCard(v.tasks[s.TaskID], s, worktrees, nil).PRs)
	}
	return out
}

func board(prs []domain.PullRequest) []BoardPR {
	out := make([]BoardPR, 0, len(prs))
	for _, pr := range prs {
		b := BoardPR{
			Number: pr.Number, Title: pr.Title, URL: pr.URL, Branch: pr.Head, State: string(pr.State),
			Checks: string(pr.Checks), FailingChecks: []FailingCheck{},
			ReviewDecision: string(pr.ReviewDecision), UnresolvedThreads: pr.UnresolvedThreads,
			BotComments: pr.BotComments, Mergeable: string(pr.Mergeable),
			ReadyToMerge: pr.ReadyToMerge(), Blockers: orEmpty(pr.Blockers()),
		}
		for _, f := range pr.Failing {
			b.FailingChecks = append(b.FailingChecks, FailingCheck{Name: f.Name, URL: f.URL})
		}
		out = append(out, b)
	}
	return out
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

func (d *NativeDiff) keep() bool {
	return d.phone().keep() || d.Event != nil || d.Subagent != nil || d.Draft != nil || d.Comment != nil || d.Reclaimable != nil
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
