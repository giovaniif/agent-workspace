package notify_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/notify"
	"github.com/giovaniif/crew/internal/domain"
)

type sink struct {
	posted  []domain.Banner
	removed []string
}

func (s *sink) Notify(_ context.Context, b domain.Banner) error {
	s.posted = append(s.posted, b)
	return nil
}

func (s *sink) Remove(_ context.Context, group string) error {
	s.removed = append(s.removed, group)
	return nil
}

type front bool

func (f front) TerminalFrontmost(context.Context) bool { return bool(f) }

const stream = `{"banner":{"Title":"fix login","Body":"done","State":"done","Group":"s1"}}
not json
{"remove":"s1"}
{"banner":{"Title":"api","Body":"waiting","State":"waiting","Group":"s2"},"focused":true}
`

func TestNotifyRelayPostsBannersAndRemovalsFromTheStream(t *testing.T) {
	s := &sink{}
	err := notify.Relay(context.Background(), strings.NewReader(stream), notify.RelayTarget{Notifier: s, Foreground: front(false), Terminal: "com.mitchellh.ghostty"})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Banner{
		{Title: "fix login", Body: "done", State: domain.StateDone, Group: "s1", Terminal: "com.mitchellh.ghostty"},
		{Title: "api", Body: "waiting", State: domain.StateWaiting, Group: "s2", Terminal: "com.mitchellh.ghostty"},
	}
	if !reflect.DeepEqual(s.posted, want) {
		t.Fatalf("posted\n got %+v\nwant %+v", s.posted, want)
	}
	if !reflect.DeepEqual(s.removed, []string{"s1"}) {
		t.Fatalf("removed %v", s.removed)
	}
}

func TestNotifyRelayDropsAFocusedSessionsBannerWhileTheTerminalIsInFront(t *testing.T) {
	s := &sink{}
	if err := notify.Relay(context.Background(), strings.NewReader(stream), notify.RelayTarget{Notifier: s, Foreground: front(true)}); err != nil {
		t.Fatal(err)
	}
	if len(s.posted) != 1 || s.posted[0].Group != "s1" {
		t.Fatalf("posted %+v", s.posted)
	}
}

type failing struct{ sink }

func (f *failing) Notify(context.Context, domain.Banner) error { return errors.New("no notifier") }

func TestNotifyRelayKeepsGoingWhenABannerFails(t *testing.T) {
	f := &failing{}
	if err := notify.Relay(context.Background(), strings.NewReader(stream), notify.RelayTarget{Notifier: f}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.removed, []string{"s1"}) {
		t.Fatalf("removed %v", f.removed)
	}
}

func TestNotifyTerminalNotifierRunsTheGivenFocusCommandOnClick(t *testing.T) {
	r := &recorder{}
	n := notify.TerminalNotifier{Run: r.run, Bin: "terminal-notifier", FocusCmd: "ssh -T 'vps' 'crew' focus"}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t", Body: "b", Group: "s1"}); err != nil {
		t.Fatal(err)
	}
	got := r.calls[0]
	if got[len(got)-2] != "-execute" || got[len(got)-1] != "ssh -T 'vps' 'crew' focus 's1'" {
		t.Fatalf("argv %q", got)
	}
}

func TestNotifySelectIsSilentWithoutAnyNotifier(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	n := notify.Select(missing, notify.Click{Self: "/bin/crew", Home: "/h"})
	if _, ok := n.(notify.Silent); !ok {
		t.Fatalf("got %#v", n)
	}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t"}); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyFallbackPostsThroughTheSecondWhenTheFirstFails(t *testing.T) {
	first, second := &failing{}, &sink{}
	n := notify.Fallback{Primary: first, Secondary: second}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t", Group: "s1"}); err != nil {
		t.Fatal(err)
	}
	if len(second.posted) != 1 || second.posted[0].Title != "t" {
		t.Fatalf("second posted %+v", second.posted)
	}
	if err := n.Remove(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.removed, []string{"s1"}) {
		t.Fatalf("first removed %v", first.removed)
	}
}

func TestNotifyFallbackLeavesTheSecondAloneWhenTheFirstWorks(t *testing.T) {
	first, second := &sink{}, &sink{}
	n := notify.Fallback{Primary: first, Secondary: second}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if len(first.posted) != 1 || len(second.posted) != 0 {
		t.Fatalf("first %+v second %+v", first.posted, second.posted)
	}
}

func TestNotifyFallbackReportsBothFailures(t *testing.T) {
	n := notify.Fallback{Primary: &failing{}, Secondary: &failing{}}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t"}); err == nil {
		t.Fatal("no error with both backends failing")
	}
}
