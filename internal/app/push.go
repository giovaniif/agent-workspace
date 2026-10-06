package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

var ErrPushGone = errors.New("the push service no longer has this subscription")

type PushProvider interface {
	PublicKey() (string, error)
	Send(ctx context.Context, sub domain.PushSubscription, msg domain.PushMessage) error
}

type TerminalActivity interface {
	LastInput(ctx context.Context) (time.Time, error)
}

func SendPush(ctx context.Context, p PushProvider, subs []domain.PushSubscription, msg domain.PushMessage) ([]domain.PushSubscription, error) {
	errs := make([]error, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Go(func() { errs[i] = p.Send(ctx, sub, msg) })
	}
	wg.Wait()
	var gone []domain.PushSubscription
	var failed []error
	for i, err := range errs {
		switch {
		case errors.Is(err, ErrPushGone):
			gone = append(gone, subs[i])
		case err != nil:
			failed = append(failed, err)
		}
	}
	return gone, errors.Join(failed...)
}
