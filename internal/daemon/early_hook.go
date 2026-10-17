package daemon

import (
	"time"

	"github.com/giovaniif/crew/internal/rpc"
)

const (
	earlyHookTTL      = 30 * time.Second
	earlyHooksPerPane = 16
)

type earlyHook struct {
	hook rpc.Hook
	at   time.Time
}

func (s *state) holdEarlyHook(h rpc.Hook, now time.Time) {
	if h.Pane == "" {
		return
	}
	for pane, held := range s.earlyHooks {
		if len(held) == 0 || now.Sub(held[len(held)-1].at) > earlyHookTTL {
			delete(s.earlyHooks, pane)
		}
	}
	held := append(s.earlyHooks[h.Pane], earlyHook{hook: h, at: now})
	if len(held) > earlyHooksPerPane {
		held = held[len(held)-earlyHooksPerPane:]
	}
	s.earlyHooks[h.Pane] = held
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
