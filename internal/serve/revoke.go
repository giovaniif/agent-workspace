package serve

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/giovaniif/crew/internal/rpc"
)

var errRevoked = errors.New("device revoked")

const resubscribeEvery = time.Second

type registry struct {
	mu      sync.Mutex
	next    uint64
	revoked map[string]bool
	open    map[string]map[uint64]context.CancelCauseFunc
}

func newRegistry() *registry {
	return &registry{revoked: map[string]bool{}, open: map[string]map[uint64]context.CancelCauseFunc{}}
}

func (g *registry) add(device string, cancel context.CancelCauseFunc) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked[device] {
		return nil, false
	}
	g.next++
	id := g.next
	if g.open[device] == nil {
		g.open[device] = map[uint64]context.CancelCauseFunc{}
	}
	g.open[device][id] = cancel
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.open[device], id)
		if len(g.open[device]) == 0 {
			delete(g.open, device)
		}
	}, true
}

func (g *registry) revoke(device string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revoked[device] = true
	for _, cancel := range g.open[device] {
		cancel(errRevoked)
	}
	delete(g.open, device)
}

func (g *registry) isRevoked(device string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revoked[device]
}

func (s *Server) Start(ctx context.Context) error {
	s.base = ctx
	d, sub, err := s.subscribe(ctx)
	if err != nil {
		return err
	}
	go s.watchRevocations(ctx, d, sub)
	return nil
}

func (s *Server) subscribe(ctx context.Context) (Daemon, rpc.Subscription, error) {
	d, err := s.cfg.Dial(ctx)
	if err != nil {
		return nil, rpc.Subscription{}, err
	}
	sub, err := d.Subscribe(ctx)
	if err != nil {
		_ = d.Close()
		return nil, rpc.Subscription{}, err
	}
	return d, sub, nil
}

func (s *Server) watchRevocations(ctx context.Context, d Daemon, sub rpc.Subscription) {
	for {
		s.applyRevocations(ctx, sub.Diffs)
		_ = d.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(resubscribeEvery):
			}
			var err error
			if d, sub, err = s.subscribe(ctx); err == nil {
				break
			}
		}
	}
}

func (s *Server) applyRevocations(ctx context.Context, diffs <-chan rpc.Diff) {
	for {
		select {
		case <-ctx.Done():
			return
		case diff, ok := <-diffs:
			if !ok {
				return
			}
			if diff.RevokedDevice != "" {
				s.streams.revoke(diff.RevokedDevice)
			}
		}
	}
}
