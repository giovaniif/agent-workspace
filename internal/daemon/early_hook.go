package daemon

import (
	"time"

	"github.com/giovaniif/crew/internal/rpc"
)

const (
	earlyHookTTL      = 30 * time.Second
	earlyHooksPerPane = 16
	earlyHookPanes    = 64
)

type earlyHook struct {
	hook rpc.Hook
	at   time.Time
}

func (s *state) holdEarlyHook(h rpc.Hook, now time.Time) {
	if h.Pane == "" {
		return
	}
	if _, ok := s.earlyHooks[h.Pane]; !ok && len(s.earlyHooks) >= earlyHookPanes {
		s.makeRoomForEarlyHook(now)
	}
	held := append(s.earlyHooks[h.Pane], earlyHook{hook: h, at: now})
	if len(held) > earlyHooksPerPane {
		held = held[len(held)-earlyHooksPerPane:]
	}
	s.earlyHooks[h.Pane] = held
}

func (s *state) makeRoomForEarlyHook(now time.Time) {
	oldest, oldestAt := "", now
	for pane, held := range s.earlyHooks {
		last := held[len(held)-1].at
		if now.Sub(last) > earlyHookTTL {
			delete(s.earlyHooks, pane)
			continue
		}
		if !last.After(oldestAt) {
			oldest, oldestAt = pane, last
		}
	}
	if len(s.earlyHooks) >= earlyHookPanes {
		delete(s.earlyHooks, oldest)
	}
}

func (s *state) replayEarlyHooks(pane string, now time.Time) {
	held := s.earlyHooks[pane]
	delete(s.earlyHooks, pane)
	for _, e := range held {
		if now.Sub(e.at) <= earlyHookTTL {
			s.hook(e.hook, e.at)
		}
	}
}
