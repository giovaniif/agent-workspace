package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type fakeSlackHub struct {
	mu      sync.Mutex
	mates   []*fakeSlackMate
	fail    string
	replies []app.SlackMessage
	names   map[string]string
	users   map[string]string
	seq     int
}

type fakeSlackMate struct {
	hub     *fakeSlackHub
	bot     string
	appTok  string
	handle  func(app.SlackEvent)
	stopped bool
	posts   []fakeSlackPost
	updates []fakeSlackPost
	asks    []fakeSlackAsk
}

type fakeSlackAsk struct {
	channel, thread, text, id string
}

type fakeSlackPost struct {
	channel, thread, text string
}

func (h *fakeSlackHub) connect(bot, appToken string) app.SlackMate {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := &fakeSlackMate{hub: h, bot: bot, appTok: appToken}
	h.mates = append(h.mates, m)
	return m
}

func (m *fakeSlackMate) Run(ctx context.Context, handle func(app.SlackEvent), status func(app.SlackStatus)) error {
	status(app.SlackStatus{State: domain.MateConnecting})
	m.hub.mu.Lock()
	fail := m.hub.fail
	m.hub.mu.Unlock()
	if fail != "" {
		status(app.SlackStatus{State: domain.MateError, Error: fail})
		return errors.New(fail)
	}
	m.hub.mu.Lock()
	m.handle = handle
	user := m.hub.users[m.bot]
	m.hub.mu.Unlock()
	status(app.SlackStatus{State: domain.MateConnected, User: user})
	<-ctx.Done()
	m.hub.mu.Lock()
	m.stopped = true
	m.hub.mu.Unlock()
	return ctx.Err()
}

func (m *fakeSlackMate) PostMessage(_ context.Context, channel, thread, text string) (string, error) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.posts = append(m.posts, fakeSlackPost{channel, thread, text})
	m.hub.seq++
	return fmt.Sprintf("1800.%04d", m.hub.seq), nil
}

func (m *fakeSlackMate) PostApproval(_ context.Context, channel, thread, text, id string) (string, error) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.asks = append(m.asks, fakeSlackAsk{channel, thread, text, id})
	m.hub.seq++
	return fmt.Sprintf("1900.%04d", m.hub.seq), nil
}

func (m *fakeSlackMate) UpdateMessage(_ context.Context, channel, ts, text string) error {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.updates = append(m.updates, fakeSlackPost{channel, ts, text})
	return nil
}

func (m *fakeSlackMate) Replies(context.Context, string, string) ([]app.SlackMessage, error) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	return append([]app.SlackMessage(nil), m.hub.replies...), nil
}

func (m *fakeSlackMate) UserName(_ context.Context, user string) (string, error) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	if name, ok := m.hub.names[user]; ok {
		return name, nil
	}
	return "", nil
}

func (h *fakeSlackHub) live(t *testing.T) *fakeSlackMate {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, m := range h.mates {
			if m.handle != nil && !m.stopped {
				h.mu.Unlock()
				return m
			}
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no mate connected")
	return nil
}

func (h *fakeSlackHub) liveBot(t *testing.T, bot string) *fakeSlackMate {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, m := range h.mates {
			if m.bot == bot && m.handle != nil && !m.stopped {
				h.mu.Unlock()
				return m
			}
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no mate connected with %s", bot)
	return nil
}

func (h *fakeSlackHub) postsOf(m *fakeSlackMate) []fakeSlackPost {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeSlackPost(nil), m.posts...)
}

func (h *fakeSlackHub) send(m *fakeSlackMate, e app.SlackEvent) {
	h.mu.Lock()
	handle := m.handle
	h.mu.Unlock()
	handle(e)
}

func (h *fakeSlackHub) snapshot() []fakeSlackMate {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]fakeSlackMate, len(h.mates))
	for i, m := range h.mates {
		out[i] = fakeSlackMate{bot: m.bot, appTok: m.appTok, stopped: m.stopped, posts: append([]fakeSlackPost(nil), m.posts...), asks: append([]fakeSlackAsk(nil), m.asks...), updates: append([]fakeSlackPost(nil), m.updates...)}
	}
	return out
}
