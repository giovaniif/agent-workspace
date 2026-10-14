package app_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestSendPushReachesEverySubscriptionOnce(t *testing.T) {
	p := &fakePush{}
	msg := domain.PushMessage{Title: "api", Body: "needs permission", URL: "/#/sessions/s1", Tag: "s1"}
	subs := []domain.PushSubscription{{Endpoint: "https://p.example/1"}, {Endpoint: "https://p.example/2"}}
	gone, err := app.SendPush(context.Background(), p, subs, msg)
	if err != nil || len(gone) != 0 {
		t.Fatalf("gone %v err %v", gone, err)
	}
	if len(p.sent) != 2 {
		t.Fatalf("sent %d pushes", len(p.sent))
	}
	want := map[string]domain.PushMessage{"https://p.example/1": msg, "https://p.example/2": msg}
	if got := p.endpoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %+v", got)
	}
}

func TestSendPushReportsGoneSubscriptionsAndKeepsSending(t *testing.T) {
	p := &fakePush{errs: map[string]error{
		"https://p.example/gone":   fmt.Errorf("410 Gone: %w", app.ErrPushGone),
		"https://p.example/broken": errors.New("503 Service Unavailable"),
	}}
	subs := []domain.PushSubscription{
		{Endpoint: "https://p.example/gone"}, {Endpoint: "https://p.example/broken"}, {Endpoint: "https://p.example/ok"},
	}
	gone, err := app.SendPush(context.Background(), p, subs, domain.PushMessage{Title: "t", Body: "b"})
	if !reflect.DeepEqual(gone, []domain.PushSubscription{{Endpoint: "https://p.example/gone"}}) {
		t.Fatalf("gone %v", gone)
	}
	if err == nil || errors.Is(err, app.ErrPushGone) {
		t.Fatalf("err %v", err)
	}
	if len(p.sent) != 3 {
		t.Fatalf("sent %d pushes", len(p.sent))
	}
}

func TestSendPushWithNoSubscriptionsSendsNothing(t *testing.T) {
	p := &fakePush{}
	if gone, err := app.SendPush(context.Background(), p, nil, domain.PushMessage{Title: "t", Body: "b"}); err != nil || gone != nil {
		t.Fatalf("gone %v err %v", gone, err)
	}
	if len(p.sent) != 0 {
		t.Fatalf("sent %+v", p.sent)
	}
}
