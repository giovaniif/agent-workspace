package daemon

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	pushQueue   = 64
	pushTimeout = 20 * time.Second
)

func WithPush(p app.PushProvider) Option {
	return func(d *Daemon) {
		d.push = p
		d.st.pushes = make(chan domain.PushMessage, pushQueue)
	}
}

func (s *state) queuePush(b domain.Banner) {
	if s.pushes == nil || !s.gate.Admit(b, s.presence, time.Now()) {
		return
	}
	s.enqueuePush(b)
}

func (s *state) enqueuePush(b domain.Banner) {
	select {
	case s.pushes <- domain.PushFor(b):
	default:
		log.Printf("push: queue full, dropped a push for %s", b.Group)
	}
}

func (d *Daemon) runPush(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-d.st.pushes:
			d.deliverPush(ctx, msg)
		}
	}
}

func (d *Daemon) deliverPush(ctx context.Context, msg domain.PushMessage) {
	var subs []domain.PushSubscription
	if !d.query(func(s *state) { subs = domain.PushTargets(sorted(s.devices), s.viewing(time.Now())) }) || len(subs) == 0 {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, pushTimeout)
	gone, err := app.SendPush(sctx, d.push, subs, msg)
	cancel()
	if err != nil {
		log.Printf("push: %v", err)
	}
	for _, sent := range gone {
		d.query(func(s *state) { s.putDevices(domain.DropPushSubscription(sorted(s.devices), sent)) })
	}
}

func (s *state) putDevices(devices []domain.Device) {
	for _, dev := range devices {
		s.devices[dev.ID] = dev
		s.store.PutDevice(dev)
	}
}

func (d *Daemon) pushMethod(req rpc.Request) (*rpc.Response, bool) {
	if d.push == nil {
		return errorResponse(req.ID, rpc.CodeUnknownMethod, "unknown method "+req.Method), true
	}
	if req.Method == rpc.MethodPushKey {
		key, err := d.push.PublicKey()
		if err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, "no VAPID key: "+err.Error()), true
		}
		return result(req.ID, rpc.PushKey{PublicKey: key}), true
	}
	if req.Method == rpc.MethodPushUnsubscribe {
		return d.pushUnsubscribe(req)
	}
	var p rpc.PushSubscribeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "push.subscribe params: "+err.Error()), true
	}
	sub := domain.PushSubscription{Endpoint: p.Endpoint, P256dh: p.Keys.P256dh, Auth: p.Keys.Auth}
	if err := domain.CheckPushSubscription(sub); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, err.Error()), true
	}
	var found bool
	ok := d.query(func(s *state) {
		var changed []domain.Device
		if changed, found = domain.SubscribePush(sorted(s.devices), p.Device, sub); found {
			s.putDevices(changed)
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no device "+p.Device), ok
	}
	return result(req.ID, struct{}{}), ok
}

func (d *Daemon) pushUnsubscribe(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.PushUnsubscribeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "push.unsubscribe params: "+err.Error()), true
	}
	var found bool
	ok := d.query(func(s *state) {
		var changed []domain.Device
		if changed, found = domain.UnsubscribePush(sorted(s.devices), p.Device); found {
			s.putDevices(changed)
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no device "+p.Device), ok
	}
	return result(req.ID, struct{}{}), ok
}
