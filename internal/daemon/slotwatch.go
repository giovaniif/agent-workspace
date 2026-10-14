package daemon

import (
	"context"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

const slotWatchEvery = 2 * time.Second

func WithSlotWatch(every time.Duration) Option {
	return func(d *Daemon) { d.clients.watchEvery = every }
}

func (d *Daemon) watchMainSlot(ctx context.Context) {
	tick, stop := ticker(d.clients.watchEvery)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			d.refillLostSlot(ctx)
		}
	}
}

func (d *Daemon) refillLostSlot(ctx context.Context) {
	d.clients.mu.Lock()
	host, slot := d.clients.host, d.clients.slot
	d.clients.mu.Unlock()
	if host == nil || slot == "" {
		return
	}
	inView, found, ok := d.sessionInView()
	if !ok || host.SlotHasPane(ctx, slot) {
		return
	}
	if !found {
		d.refillMain(domain.Session{}, false)
		return
	}
	if d.restoreLiveAgent(ctx, inView, slot) {
		return
	}
	_, _, _ = d.endAndRefill(inView)
}

func (d *Daemon) restoreLiveAgent(ctx context.Context, inView domain.Session, slot app.Slot) bool {
	d.clients.mu.Lock()
	defer d.clients.mu.Unlock()
	if now, stillFound, ok := d.sessionInView(); !ok || !stillFound || now.ID != inView.ID || now.Pane != inView.Pane {
		return true
	}
	if d.hs.host == nil || inView.Pane == "" {
		return false
	}
	alive, err := d.hs.host.Alive(ctx, app.PaneID(inView.Pane))
	if err != nil {
		return true
	}
	if alive {
		_ = d.hs.host.Show(ctx, app.PaneID(inView.Pane), slot)
	}
	return alive
}

func (d *Daemon) sessionInView() (session domain.Session, found, ok bool) {
	ok = d.query(func(s *state) {
		for _, cur := range sorted(s.sessions) {
			if cur.Focused {
				session, found = cur, true
			}
		}
	})
	return session, found, ok
}
