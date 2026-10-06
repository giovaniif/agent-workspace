package domain

import (
	"sort"
	"time"
)

const (
	DefaultAwayAfter = 2 * time.Minute
	ViewingFresh     = 90 * time.Second
)

type Presence struct {
	AwayAfter time.Duration
	LastInput time.Time
}

func (p Presence) AtTerminal(now time.Time) bool {
	return p.AwayAfter > 0 && !p.LastInput.IsZero() && now.Sub(p.LastInput) < p.AwayAfter
}

func NeedsYou(s Session) bool {
	if s.Muted || s.Ended {
		return false
	}
	switch s.State {
	case StatePermission, StateWaiting:
		return true
	case StateDone:
		return s.Unread
	}
	return false
}

type PushGate struct {
	held map[string]Banner
}

func NewPushGate() *PushGate {
	return &PushGate{held: map[string]Banner{}}
}

func (g *PushGate) Admit(b Banner, p Presence, now time.Time) bool {
	if p.AtTerminal(now) {
		g.held[b.Group] = b
		return false
	}
	delete(g.held, b.Group)
	return true
}

func (g *PushGate) Release(sessions []Session, p Presence, now time.Time) []Banner {
	if p.AtTerminal(now) {
		return nil
	}
	byID := make(map[string]Session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	ids := make([]string, 0, len(g.held))
	for id := range g.held {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Banner
	for _, id := range ids {
		if s, ok := byID[id]; ok && NeedsYou(s) {
			out = append(out, g.held[id])
		}
	}
	clear(g.held)
	return out
}

type ViewReport struct {
	Device  string
	Visible bool
	At      time.Time
}

func Viewing(reports []ViewReport, now time.Time) map[string]bool {
	out := map[string]bool{}
	for _, r := range reports {
		if r.Visible && now.Sub(r.At) < ViewingFresh {
			out[r.Device] = true
		}
	}
	return out
}
