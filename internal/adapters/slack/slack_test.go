package slack_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/crew/internal/adapters/slack"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

const (
	testBot = "xoxb-1-2-zzbotsecretzz"
	testApp = "xapp-1-A-3-zzappsecretzz"
)

type apiCall struct {
	method string
	auth   string
	form   map[string]string
}

type fakeSlack struct {
	srv     *httptest.Server
	mu      sync.Mutex
	script  [][]string
	opens   int
	openErr string
	acks    []string
	calls   []apiCall
}

func newFakeSlack(t *testing.T, script ...[]string) *fakeSlack {
	t.Helper()
	f := &fakeSlack{script: script}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) client() *slack.Client {
	c := slack.New(testBot, testApp)
	c.API = f.srv.URL + "/api/"
	c.Backoff = func(int) time.Duration { return time.Millisecond }
	return c
}

func (f *fakeSlack) reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		f.socket(w, r)
		return
	}
	_ = r.ParseForm()
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{method: method, auth: r.Header.Get("Authorization"), form: form})
	openErr := f.openErr
	f.mu.Unlock()
	switch method {
	case "apps.connections.open":
		if openErr != "" {
			f.reply(w, map[string]any{"ok": false, "error": openErr})
			return
		}
		f.reply(w, map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws"})
	case "auth.test":
		f.reply(w, map[string]any{"ok": true, "user_id": "UBOT00001", "bot_id": "BBOT0001"})
	case "chat.postMessage":
		f.reply(w, map[string]any{"ok": true, "ts": "1700.0002"})
	case "chat.update":
		if form["ts"] == "" {
			f.reply(w, map[string]any{"ok": false, "error": "message_not_found"})
			return
		}
		f.reply(w, map[string]any{"ok": true})
	case "conversations.replies":
		f.reply(w, map[string]any{"ok": true, "messages": []map[string]any{
			{"user": "U0ANA0001", "text": "hello", "ts": "1700.0001"},
			{"user": "UBOT00001", "bot_id": "BBOT0001", "text": "on it", "ts": "1700.0002"},
		}})
	case "users.info":
		f.reply(w, map[string]any{"ok": true, "user": map[string]any{"name": "ana", "real_name": "Ana Lima", "profile": map[string]any{"display_name": "ana.l"}}})
	default:
		f.reply(w, map[string]any{"ok": false, "error": "unknown_method"})
	}
}

func (f *fakeSlack) socket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	f.mu.Lock()
	idx := f.opens
	f.opens++
	var frames []string
	if idx < len(f.script) {
		frames = f.script[idx]
	}
	f.mu.Unlock()
	ctx := r.Context()
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if json.Unmarshal(data, &ack) == nil && ack.EnvelopeID != "" {
				f.mu.Lock()
				f.acks = append(f.acks, ack.EnvelopeID)
				f.mu.Unlock()
			}
		}
	}()
	for _, frame := range frames {
		if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			return
		}
	}
	<-ctx.Done()
}

func (f *fakeSlack) snapshot() (int, []string, []apiCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens, append([]string(nil), f.acks...), append([]apiCall(nil), f.calls...)
}

const hello = `{"type":"hello","num_connections":1}`

func eventFrame(envelope, eventID, event string) string {
	return `{"type":"events_api","envelope_id":"` + envelope + `","payload":{"team_id":"T0TEAM001","event_id":"` + eventID + `","event":` + event + `}}`
}

type recorder struct {
	mu     sync.Mutex
	events []app.SlackEvent
	states []app.SlackStatus
}

func (r *recorder) handle(e app.SlackEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) status(s app.SlackStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func runUntil(t *testing.T, c *slack.Client, rec *recorder, done func() bool) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- c.Run(ctx, rec.handle, rec.status) }()
	for !done() {
		select {
		case err := <-errc:
			return err
		case <-ctx.Done():
			t.Fatal("the condition was not reached")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	return <-errc
}

func TestRunConnectsAcksEachEnvelopeAndHandsOnTheEvents(t *testing.T) {
	f := newFakeSlack(t, []string{
		hello,
		eventFrame("env1", "Ev1", `{"type":"app_mention","user":"U0ANA0001","text":"<@UBOT00001> look","channel":"C0CHAN001","ts":"1700.0001"}`),
		eventFrame("env2", "Ev2", `{"type":"message","user":"U0ANA0001","text":"more","channel":"C0CHAN001","channel_type":"channel","ts":"1700.0003","thread_ts":"1700.0001"}`),
		eventFrame("env3", "Ev3", `{"type":"message","user":"U0ANA0001","text":"ABCD2345","channel":"D0DIRECT1","channel_type":"im","ts":"1700.0004"}`),
		eventFrame("env4", "Ev4", `{"type":"message","user":"U0ANA0001","text":"not in a thread","channel":"C0CHAN001","channel_type":"channel","ts":"1700.0005"}`),
		`{"type":"interactive","envelope_id":"env5","payload":{"type":"block_actions","team":{"id":"T0TEAM001"},"user":{"id":"U012ABCDEF"},"channel":{"id":"C0CHAN001"},"message":{"ts":"1700.0006","thread_ts":"1700.0001"},"actions":[{"action_id":"approve","value":"ap1"}]}}`,
	})
	rec := &recorder{}
	c := f.client()
	if err := runUntil(t, c, rec, func() bool { _, acks, _ := f.snapshot(); return len(acks) == 5 && rec.count() == 4 }); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	_, acks, calls := f.snapshot()
	if strings.Join(acks, ",") != "env1,env2,env3,env4,env5" {
		t.Errorf("acks %v", acks)
	}
	want := []app.SlackEvent{
		{Kind: app.SlackMention, Team: "T0TEAM001", Channel: "C0CHAN001", User: "U0ANA0001", Text: "<@UBOT00001> look", TS: "1700.0001", ThreadTS: "1700.0001"},
		{Kind: app.SlackThreadMessage, Team: "T0TEAM001", Channel: "C0CHAN001", User: "U0ANA0001", Text: "more", TS: "1700.0003", ThreadTS: "1700.0001"},
		{Kind: app.SlackDirectMessage, Team: "T0TEAM001", Channel: "D0DIRECT1", User: "U0ANA0001", Text: "ABCD2345", TS: "1700.0004", ThreadTS: "1700.0004"},
		{Kind: app.SlackAction, Team: "T0TEAM001", Channel: "C0CHAN001", User: "U012ABCDEF", TS: "1700.0006", ThreadTS: "1700.0001", ActionID: "approve", Value: "ap1"},
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != len(want) {
		t.Fatalf("events %+v", rec.events)
	}
	for i := range want {
		if rec.events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, rec.events[i], want[i])
		}
	}
	if len(rec.states) < 2 || rec.states[0].State != domain.MateConnecting || rec.states[1].State != domain.MateConnected {
		t.Errorf("states %+v", rec.states)
	}
	for _, call := range calls {
		want := "Bearer " + testBot
		if call.method == "apps.connections.open" {
			want = "Bearer " + testApp
		}
		if call.auth != want {
			t.Errorf("%s used %q", call.method, call.auth)
		}
	}
}

func TestRunReconnectsAfterADisconnect(t *testing.T) {
	f := newFakeSlack(t,
		[]string{hello, `{"type":"disconnect","reason":"refresh_requested"}`},
		[]string{hello, eventFrame("env9", "Ev9", `{"type":"app_mention","user":"U0ANA0001","text":"hi","channel":"C0CHAN001","ts":"1700.0009"}`)},
	)
	rec := &recorder{}
	_ = runUntil(t, f.client(), rec, func() bool { return rec.count() == 1 })
	opens, acks, _ := f.snapshot()
	if opens != 2 || strings.Join(acks, ",") != "env9" {
		t.Errorf("opens %d, acks %v", opens, acks)
	}
}

func TestRunReconnectsWhenTheSocketCloses(t *testing.T) {
	f := newFakeSlack(t, []string{hello}, []string{hello, eventFrame("env9", "Ev9", `{"type":"app_mention","user":"U0ANA0001","text":"hi","channel":"C0CHAN001","ts":"1700.0009"}`)})
	f.srv.Config.Handler = closeFirstSocket(f)
	rec := &recorder{}
	_ = runUntil(t, f.client(), rec, func() bool { return rec.count() == 1 })
	if opens, _, _ := f.snapshot(); opens != 2 {
		t.Errorf("opens %d", opens)
	}
}

func closeFirstSocket(f *fakeSlack) http.Handler {
	var once sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		closed := false
		if r.URL.Path == "/ws" {
			once.Do(func() {
				closed = true
				f.mu.Lock()
				f.opens++
				f.mu.Unlock()
				conn, err := websocket.Accept(w, r, nil)
				if err == nil {
					_ = conn.Write(r.Context(), websocket.MessageText, []byte(hello))
					_ = conn.Close(websocket.StatusGoingAway, "bye")
				}
			})
		}
		if !closed {
			f.serve(w, r)
		}
	})
}

func TestRunDedupesARetriedEnvelope(t *testing.T) {
	mention := `{"type":"app_mention","user":"U0ANA0001","text":"hi","channel":"C0CHAN001","ts":"1700.0001"}`
	f := newFakeSlack(t, []string{
		hello,
		eventFrame("env1", "Ev1", mention),
		`{"type":"events_api","envelope_id":"env2","retry_attempt":1,"payload":{"team_id":"T0TEAM001","event_id":"Ev1","event":` + mention + `}}`,
		eventFrame("env1", "Ev1", mention),
		eventFrame("env3", "Ev2", `{"type":"app_mention","user":"U0ANA0001","text":"again","channel":"C0CHAN001","ts":"1700.0002"}`),
	})
	rec := &recorder{}
	_ = runUntil(t, f.client(), rec, func() bool { _, acks, _ := f.snapshot(); return len(acks) == 4 && rec.count() >= 2 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 2 || rec.events[0].Text != "hi" || rec.events[1].Text != "again" {
		t.Errorf("events %+v", rec.events)
	}
}

func TestRunIgnoresTheMatesOwnMessages(t *testing.T) {
	f := newFakeSlack(t, []string{
		hello,
		eventFrame("env1", "Ev1", `{"type":"message","user":"UBOT00001","text":"mine","channel":"C0CHAN001","channel_type":"channel","ts":"1700.0002","thread_ts":"1700.0001"}`),
		eventFrame("env2", "Ev2", `{"type":"message","bot_id":"BBOT0001","text":"mine too","channel":"D0DIRECT1","channel_type":"im","ts":"1700.0003"}`),
		eventFrame("env3", "Ev3", `{"type":"message","subtype":"message_changed","channel":"C0CHAN001","channel_type":"channel","ts":"1700.0004","thread_ts":"1700.0001"}`),
		eventFrame("env4", "Ev4", `{"type":"message","user":"U0ANA0001","text":"theirs","channel":"C0CHAN001","channel_type":"channel","ts":"1700.0005","thread_ts":"1700.0001"}`),
	})
	rec := &recorder{}
	_ = runUntil(t, f.client(), rec, func() bool { _, acks, _ := f.snapshot(); return len(acks) == 4 && rec.count() >= 1 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 1 || rec.events[0].Text != "theirs" {
		t.Errorf("events %+v", rec.events)
	}
}

func TestRunStopsOnATokenErrorAndKeepsTheTokenOutOfIt(t *testing.T) {
	f := newFakeSlack(t)
	f.openErr = "invalid_auth"
	rec := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := f.client().Run(ctx, rec.handle, rec.status)
	if !errors.Is(err, slack.ErrToken) {
		t.Fatalf("Run = %v, want ErrToken", err)
	}
	last := rec.states[len(rec.states)-1]
	if last.State != domain.MateError || !strings.Contains(last.Error, "invalid_auth") {
		t.Errorf("last state %+v", last)
	}
	for _, text := range []string{err.Error(), last.Error} {
		if strings.Contains(text, "zz") || strings.Contains(text, "xapp-") || strings.Contains(text, "xoxb-") {
			t.Errorf("%q holds a token", text)
		}
	}
}

func TestRunRetriesAnOpenErrorThatIsNotAboutTheToken(t *testing.T) {
	f := newFakeSlack(t, []string{hello, eventFrame("env1", "Ev1", `{"type":"app_mention","user":"U0ANA0001","text":"hi","channel":"C0CHAN001","ts":"1700.0001"}`)})
	f.openErr = "ratelimited"
	rec := &recorder{}
	go func() {
		time.Sleep(30 * time.Millisecond)
		f.mu.Lock()
		f.openErr = ""
		f.mu.Unlock()
	}()
	_ = runUntil(t, f.client(), rec, func() bool { return rec.count() == 1 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	sawError := false
	for _, s := range rec.states {
		sawError = sawError || s.State == domain.MateError && strings.Contains(s.Error, "ratelimited")
	}
	if !sawError {
		t.Errorf("states %+v", rec.states)
	}
}

func TestWebAPICallsUseTheBotToken(t *testing.T) {
	f := newFakeSlack(t)
	c := f.client()
	ctx := context.Background()
	ts, err := c.PostMessage(ctx, "C0CHAN001", "1700.0001", "working on it")
	if err != nil || ts != "1700.0002" {
		t.Fatalf("PostMessage = %q, %v", ts, err)
	}
	if err := c.UpdateMessage(ctx, "C0CHAN001", "1700.0002", "done"); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.Replies(ctx, "C0CHAN001", "1700.0001")
	if err != nil {
		t.Fatal(err)
	}
	wantMsgs := []app.SlackMessage{{User: "U0ANA0001", Text: "hello", TS: "1700.0001"}, {User: "UBOT00001", Text: "on it", TS: "1700.0002", Bot: true}}
	if len(msgs) != 2 || msgs[0] != wantMsgs[0] || msgs[1] != wantMsgs[1] {
		t.Errorf("replies %+v", msgs)
	}
	name, err := c.UserName(ctx, "U0ANA0001")
	if err != nil || name != "ana.l" {
		t.Errorf("UserName = %q, %v", name, err)
	}
	_, _, calls := f.snapshot()
	want := []apiCall{
		{"chat.postMessage", "Bearer " + testBot, map[string]string{"channel": "C0CHAN001", "thread_ts": "1700.0001", "text": "working on it"}},
		{"chat.update", "Bearer " + testBot, map[string]string{"channel": "C0CHAN001", "ts": "1700.0002", "text": "done"}},
		{"conversations.replies", "Bearer " + testBot, map[string]string{"channel": "C0CHAN001", "ts": "1700.0001"}},
		{"users.info", "Bearer " + testBot, map[string]string{"user": "U0ANA0001"}},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls %+v", calls)
	}
	for i := range want {
		if calls[i].method != want[i].method || calls[i].auth != want[i].auth || len(calls[i].form) != len(want[i].form) {
			t.Errorf("call %d = %+v, want %+v", i, calls[i], want[i])
			continue
		}
		for k, v := range want[i].form {
			if calls[i].form[k] != v {
				t.Errorf("call %d %s = %q, want %q", i, k, calls[i].form[k], v)
			}
		}
	}
}

func TestWebAPIErrorNamesTheSlackErrorAndNotTheToken(t *testing.T) {
	f := newFakeSlack(t)
	err := f.client().UpdateMessage(context.Background(), "C0CHAN001", "", "x")
	if err == nil || !strings.Contains(err.Error(), "message_not_found") || strings.Contains(err.Error(), "zz") {
		t.Errorf("UpdateMessage = %v", err)
	}
}
