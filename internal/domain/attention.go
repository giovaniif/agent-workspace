package domain

import (
	"strings"
	"time"
)

// CoalesceWindow is how long a session stays quiet after a banner.
const CoalesceWindow = 10 * time.Second

// Banner is one desktop notification. Sound is filled in by whoever owns
// the per-event sound settings.
type Banner struct {
	Title string
	Body  string
	State AgentState
	Sound string
}

var bannerBody = map[AgentState]string{
	StatePermission: "needs permission",
	StateWaiting:    "waiting",
	StateDone:       "done",
}

// BannerFor turns a notify effect into a banner titled with the session's
// name. Muted sessions and other effects give none.
func BannerFor(s Session, name string, e Effect) (Banner, bool) {
	body, ok := bannerBody[e.State]
	if e.Kind != EffectNotify || s.Muted || !ok {
		return Banner{}, false
	}
	title := strings.TrimSpace(name)
	if title == "" {
		title = string(s.Harness)
	}
	return Banner{Title: title, Body: body, State: e.State}, true
}

func (s Session) SetMuted(muted bool) Session {
	s.Muted = muted
	return s
}

// Coalescer lets one banner per session through each CoalesceWindow.
type Coalescer struct {
	last map[string]time.Time
}

func NewCoalescer() *Coalescer {
	return &Coalescer{last: map[string]time.Time{}}
}

func (c *Coalescer) Allow(sessionID string, now time.Time) bool {
	if at, ok := c.last[sessionID]; ok && now.Sub(at) < CoalesceWindow {
		return false
	}
	c.last[sessionID] = now
	return true
}

// proof: throwaway
var _ = 0
