package daemon_test

import (
	"context"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type pushed struct {
	endpoint string
	msg      domain.PushMessage
}

type fakePushProvider struct {
	mu   sync.Mutex
	sent chan pushed
	errs map[string]error
}

func newFakePushProvider() *fakePushProvider {
	return &fakePushProvider{sent: make(chan pushed, 64), errs: map[string]error{}}
}

func (f *fakePushProvider) PublicKey() (string, error) { return "BFakeVapidPublicKey", nil }

func (f *fakePushProvider) Send(_ context.Context, sub domain.PushSubscription, msg domain.PushMessage) error {
	f.mu.Lock()
	err := f.errs[sub.Endpoint]
	f.mu.Unlock()
	f.sent <- pushed{endpoint: sub.Endpoint, msg: msg}
	return err
}

func (f *fakePushProvider) fail(endpoint string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[endpoint] = err
}

type fakeActivity struct {
	mu      sync.Mutex
	at      time.Time
	sampled chan struct{}
}

func newFakeActivity(at time.Time) *fakeActivity {
	return &fakeActivity{at: at, sampled: make(chan struct{}, 1)}
}

func (f *fakeActivity) LastInput(context.Context) (time.Time, error) {
	f.mu.Lock()
	at := f.at
	f.mu.Unlock()
	select {
	case f.sampled <- struct{}{}:
	default:
	}
	return at, nil
}

func (f *fakeActivity) set(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at = at
}
