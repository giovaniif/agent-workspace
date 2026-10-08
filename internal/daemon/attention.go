package daemon

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	bannerQueue   = 64
	bannerTimeout = 5 * time.Second
)

type queuedBanner struct {
	banner  domain.Banner
	focused bool
	gen     uint64
}

type attention struct {
	notifier  app.Notifier
	fg        app.Foreground
	sounds    map[domain.AgentState]string
	queue     chan queuedBanner
	terminal  atomic.Pointer[string]
	posted    map[string]bool
	removeMu  sync.Mutex
	removals  map[string]bool
	gens      map[string]uint64
	withdrawn map[string]uint64
	wake      chan struct{}
}

func WithNotifier(n app.Notifier, fg app.Foreground, sounds map[domain.AgentState]string) Option {
	return func(d *Daemon) {
		d.st.attn = &attention{
			notifier:  n,
			fg:        fg,
			sounds:    sounds,
			queue:     make(chan queuedBanner, bannerQueue),
			posted:    map[string]bool{},
			removals:  map[string]bool{},
			gens:      map[string]uint64{},
			withdrawn: map[string]uint64{},
			wake:      make(chan struct{}, 1),
		}
	}
}

func (a *attention) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case q := <-a.queue:
			a.deliver(ctx, q)
		case <-a.wake:
		}
		a.removePending(ctx)
	}
}

func (a *attention) deliver(ctx context.Context, q queuedBanner) {
	ctx, cancel := context.WithTimeout(ctx, bannerTimeout)
	defer cancel()
	if a.superseded(q) {
		return
	}
	if t := a.terminal.Load(); t != nil {
		q.banner.Terminal = *t
	}
	if q.focused && a.fg != nil && a.fg.TerminalFrontmost(ctx) {
		return
	}
	if err := a.notifier.Notify(ctx, q.banner); err != nil {
		log.Printf("notify: %v", err)
	}
}

func (s *state) announce(session domain.Session, effects []domain.Effect) {
	if s.attn == nil && s.pushes == nil {
		return
	}
	name := s.sessionName(session)
	var worktrees []domain.Worktree
	for _, id := range session.WorksIn() {
		if w, ok := s.worktrees[id]; ok {
			worktrees = append(worktrees, w)
		}
	}
	for _, e := range effects {
		b, ok := domain.BannerFor(domain.BannerInput{
			Session: session, Name: name, Effect: e,
			Worktrees: worktrees, Events: s.events[session.ID], Now: time.Now(),
		})
		if !ok || domain.InView(s.clientViews(), session.ID) || !s.co.Allow(session.ID, time.Now()) {
			continue
		}
		b.Group = session.ID
		s.queuePush(b)
		if s.attn == nil {
			continue
		}
		b.Sound = s.attn.sounds[b.State]
		if s.attn.enqueue(queuedBanner{banner: b, focused: session.Focused}) {
			s.attn.posted[session.ID] = true
		}
		s.notice(rpc.Notice{Banner: &b, Focused: session.Focused})
	}
}

func (s *state) withdraw(id string) {
	if s.attn == nil || !s.attn.posted[id] {
		return
	}
	delete(s.attn.posted, id)
	s.notice(rpc.Notice{Remove: id})
	s.attn.removeMu.Lock()
	s.attn.removals[id] = true
	s.attn.withdrawn[id] = s.attn.gens[id]
	s.attn.removeMu.Unlock()
	select {
	case s.attn.wake <- struct{}{}:
	default:
	}
}

func (s *state) notice(n rpc.Notice) {
	for c, id := range s.noticeSubs {
		if !c.push(rpc.Response{V: rpc.Version, ID: id, Notice: &n}) {
			delete(s.noticeSubs, c)
		}
	}
}

func (a *attention) enqueue(q queuedBanner) bool {
	id := q.banner.Group
	a.removeMu.Lock()
	defer a.removeMu.Unlock()
	q.gen = a.gens[id] + 1
	select {
	case a.queue <- q:
	default:
		return false
	}
	a.gens[id] = q.gen
	delete(a.removals, id)
	return true
}

func (a *attention) superseded(q queuedBanner) bool {
	a.removeMu.Lock()
	defer a.removeMu.Unlock()
	return q.gen <= a.withdrawn[q.banner.Group]
}

func (a *attention) removePending(ctx context.Context) {
	a.removeMu.Lock()
	ids := make([]string, 0, len(a.removals))
	for id := range a.removals {
		ids = append(ids, id)
	}
	clear(a.removals)
	a.removeMu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		rctx, cancel := context.WithTimeout(ctx, bannerTimeout)
		if err := a.notifier.Remove(rctx, id); err != nil {
			log.Printf("notify remove: %v", err)
		}
		cancel()
	}
}

func (a *attention) setTerminal(bundle string) {
	if a != nil {
		a.terminal.Store(&bundle)
	}
}

func (s *state) sessionName(session domain.Session) string {
	var prs []domain.PullRequest
	for _, id := range session.WorktreeIDs {
		if w, ok := s.worktrees[id]; ok && w.PR != nil {
			prs = append(prs, *w.PR)
		}
	}
	return domain.NameFor(s.tasks[session.TaskID], prs)
}

func (d *Daemon) dispatchAttention(req rpc.Request) (*rpc.Response, bool) {
	var id string
	var apply func(s *state, session domain.Session)
	switch req.Method {
	case rpc.MethodSessionMute:
		var p rpc.SessionMuteParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "session.mute params: "+err.Error()), true
		}
		id = p.ID
		apply = func(s *state, session domain.Session) { s.emit(SessionChanged{Session: session.SetMuted(p.Muted)}) }
	default:
		var p rpc.SessionFocusParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "session.focus params: "+err.Error()), true
		}
		id = p.ID
		apply = func(s *state, session domain.Session) { s.focus(session) }
	}
	found := false
	ok := d.query(func(s *state) {
		session, exists := s.sessions[id]
		if !exists {
			return
		}
		found = true
		apply(s, session)
	})
	if ok && !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+id), true
	}
	return result(req.ID, struct{}{}), ok
}

func (s *state) focus(session domain.Session) {
	s.withdraw(session.ID)
	for _, other := range sorted(s.sessions) {
		if other.ID != session.ID && other.Focused {
			s.emit(SessionChanged{Session: other.Blur()})
		}
	}
	if !session.Focused || session.Unread {
		s.emit(SessionChanged{Session: session.Focus()})
	}
	s.noteActiveTab(session)
}
