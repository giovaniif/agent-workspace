package app_test

import (
	"context"
	"sync"

	"github.com/giovaniif/crew/internal/domain"
)

type pushSent struct {
	sub domain.PushSubscription
	msg domain.PushMessage
}

type fakePush struct {
	mu   sync.Mutex
	sent []pushSent
	errs map[string]error
}

func (f *fakePush) PublicKey() (string, error) { return "BPublic", nil }

func (f *fakePush) Send(_ context.Context, sub domain.PushSubscription, msg domain.PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, pushSent{sub: sub, msg: msg})
	return f.errs[sub.Endpoint]
}

func (f *fakePush) endpoints() map[string]domain.PushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]domain.PushMessage{}
	for _, s := range f.sent {
		out[s.sub.Endpoint] = s.msg
	}
	return out
}
