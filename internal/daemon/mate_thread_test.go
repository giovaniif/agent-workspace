package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/secrets"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const threadMates = `
[[mate]]
name = "helper"
prompt = "You help with the api."
agent = "claude"
repos = ["api"]
owner = "U0OWNER01"

[[mate]]
name = "webby"
prompt = "You help with the web."
agent = "claude"
repos = ["web"]
`

const (
	mateOwner = "U0OWNER01"
	mateGuest = "U0GUEST01"
)

type mateRig struct {
	c     *rpc.Client
	hub   *fakeSlackHub
	store *memStore
	wts   *fakeWorktrees
	host  *fakeHost
}

func startMateRig(t *testing.T, store *memStore, extra ...daemon.Option) mateRig {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(threadMates), 0o600); err != nil {
		t.Fatal(err)
	}
	r := mateRig{hub: &fakeSlackHub{names: map[string]string{mateOwner: "Olivia", mateGuest: "Gus"}}, store: store, wts: &fakeWorktrees{}}
	store.snap.Workspaces = append(store.snap.Workspaces, singleWS, orchWS)
	host := &fakeHost{}
	r.host = host
	for _, s := range store.snap.Sessions {
		host.panes = append(host.panes, app.PaneInfo{ID: app.PaneID(s.Pane), Alive: true})
	}
	opts := []daemon.Option{
		daemon.WithHarnesses(host, claude.Adapter{}),
		daemon.WithSessions(r.wts, nil, "/h/worktrees"),
		daemon.WithFirstPromptGrace(time.Hour),
		daemon.WithConfig(path),
		daemon.WithMateSecrets(secrets.Files{Dir: filepath.Join(home, "secrets")}),
		daemon.WithSlack(r.hub.connect, time.Hour),
	}
	_, sock := start(t, store, append(opts, extra...)...)
	r.c = dial(t, sock)
	return r
}

func (r mateRig) connect(t *testing.T, name string) *fakeSlackMate {
	t.Helper()
	if _, err := setTokens(r.c, name, knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	return r.hub.live(t)
}

func (r mateRig) posts() []fakeSlackPost {
	m := r.hub.snapshot()
	if len(m) == 0 {
		return nil
	}
	return m[len(m)-1].posts
}

func (r mateRig) updates() []fakeSlackPost {
	m := r.hub.snapshot()
	if len(m) == 0 {
		return nil
	}
	return m[len(m)-1].updates
}

func (r mateRig) mateSessions(t *testing.T) []domain.Session {
	t.Helper()
	sub, err := r.c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Session
	for _, s := range sub.State.Sessions {
		if s.Mate != nil {
			out = append(out, s)
		}
	}
	return out
}

func (r mateRig) typed(text string) bool {
	r.host.mu.Lock()
	defer r.host.mu.Unlock()
	return strings.Contains(strings.Join(r.host.typed, "\n"), text)
}

func (r mateRig) entered(n int) bool {
	r.host.mu.Lock()
	defer r.host.mu.Unlock()
	got := 0
	for _, x := range r.host.typed {
		if strings.Contains(x, " keys ") {
			got++
		}
	}
	return got >= n
}

func mention(user, text, ts string) app.SlackEvent {
	return app.SlackEvent{Kind: app.SlackMention, Team: "T0TEAM001", Channel: "C0CHAN001", User: user, Text: text, TS: ts, ThreadTS: ts}
}

func threadReply(kind app.SlackEventKind, user, text, ts string) app.SlackEvent {
	return app.SlackEvent{Kind: kind, Team: "T0TEAM001", Channel: "C0CHAN001", User: user, Text: text, TS: ts, ThreadTS: "1700.0001"}
}

func hookMatePane(t *testing.T, c *rpc.Client, event, payload string) {
	t.Helper()
	h := rpc.Hook{Harness: "claude", Event: event, Pane: "%7", At: time.Now(), Payload: json.RawMessage(payload)}
	if err := c.Call(context.Background(), rpc.MethodHook, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMateMentionStartsASessionInARepoOfTheMateWithTheThreadAsData(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "helper")
	r.hub.mu.Lock()
	r.hub.replies = []app.SlackMessage{
		{User: mateOwner, Text: "The login is slow.", TS: "1700.0000"},
		{User: mateGuest, Text: "<@U0BOT0001> please look", TS: "1700.0001"},
	}
	r.hub.mu.Unlock()
	r.hub.send(m, mention(mateGuest, "<@U0BOT0001> please look", "1700.0001"))
	var sessions []domain.Session
	waitFor(t, func() bool { sessions = r.mateSessions(t); return len(sessions) == 1 })
	seat := sessions[0].Mate
	if seat.Name != "helper" || seat.Owner != mateOwner || seat.Authority != mateGuest {
		t.Errorf("seat %+v", seat)
	}
	r.wts.mu.Lock()
	added := append([]addedWorktree(nil), r.wts.added...)
	r.wts.mu.Unlock()
	if len(added) != 1 || added[0].repo != "/src/api" {
		t.Errorf("worktrees %+v", added)
	}
	texts := queuedTexts(t, r.c)
	if len(texts) != 1 {
		t.Fatalf("queued %q", texts)
	}
	for _, want := range []string{"You help with the api.", "Asked by: Gus", "Olivia: The login is slow.", "<slack-thread>"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("first prompt misses %q:\n%s", want, texts[0])
		}
	}
	waitFor(t, func() bool { return len(r.posts()) == 1 })
	if p := r.posts()[0]; p.channel != "C0CHAN001" || p.thread != "1700.0001" {
		t.Errorf("status post %+v", p)
	}
	waitFor(t, func() bool { return len(r.store.mateThreads()) == 1 })
	link := r.store.mateThreads()[0]
	if link.SessionID != sessions[0].ID || link.ThreadTS != "1700.0001" || link.Mate != "helper" || link.Team != "T0TEAM001" {
		t.Errorf("link %+v", link)
	}
}

func TestMateThreadReplyGoesToTheSameSessionOnceAndSetsTheAuthority(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "helper")
	r.hub.send(m, mention(mateGuest, "<@U0BOT0001> please look", "1700.0001"))
	waitFor(t, func() bool { return len(r.mateSessions(t)) == 1 && len(r.store.mateThreads()) == 1 })
	r.hub.send(m, threadReply(app.SlackThreadMessage, mateOwner, "<@U0BOT0001> also the cache", "1700.0005"))
	r.hub.send(m, threadReply(app.SlackMention, mateOwner, "<@U0BOT0001> also the cache", "1700.0005"))
	waitFor(t, func() bool { return len(queuedTexts(t, r.c)) >= 2 })
	time.Sleep(50 * time.Millisecond)
	texts := queuedTexts(t, r.c)
	if len(texts) != 2 || !strings.Contains(texts[1], "Olivia: <@U0BOT0001> also the cache") {
		t.Fatalf("queued %q", texts)
	}
	sessions := r.mateSessions(t)
	if len(sessions) != 1 {
		t.Fatalf("sessions %+v", sessions)
	}
}

func TestMateReplyFromTheOwnerWhileIdleGivesTheOwnerAuthority(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "helper")
	r.hub.send(m, mention(mateGuest, "<@U0BOT0001> please look", "1700.0001"))
	waitFor(t, func() bool { return len(r.mateSessions(t)) == 1 && len(r.store.mateThreads()) == 1 })
	finishTurn := func(sent string) {
		hookMatePane(t, r.c, "UserPromptSubmit", `{"prompt":"x"}`)
		hookMatePane(t, r.c, "Stop", `{"last_assistant_message":"Looked."}`)
		waitFor(t, func() bool { return r.typed(sent) && r.entered(1) })
		time.Sleep(20 * time.Millisecond)
		hookMatePane(t, r.c, "UserPromptSubmit", `{"prompt":"x"}`)
		hookMatePane(t, r.c, "Stop", `{"last_assistant_message":"Looked."}`)
	}
	finishTurn("please look")
	r.hub.send(m, threadReply(app.SlackThreadMessage, mateGuest, "what now", "1700.0006"))
	waitFor(t, func() bool { return r.mateSessions(t)[0].Mate.Authority == mateGuest })
	waitFor(t, func() bool { return r.typed("what now") && r.entered(2) })
	time.Sleep(20 * time.Millisecond)
	hookMatePane(t, r.c, "UserPromptSubmit", `{"prompt":"x"}`)
	hookMatePane(t, r.c, "Stop", `{"last_assistant_message":"Looked."}`)
	r.hub.send(m, threadReply(app.SlackThreadMessage, mateOwner, "merge it", "1700.0007"))
	waitFor(t, func() bool { return r.mateSessions(t)[0].Mate.Authority == mateOwner })
}

func TestMateStopPostsTheResultAndUpdatesTheStatus(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "helper")
	r.hub.send(m, mention(mateGuest, "<@U0BOT0001> please look", "1700.0001"))
	waitFor(t, func() bool { return len(r.posts()) == 1 && len(r.store.mateThreads()) == 1 })
	status := r.store.mateThreads()[0].Status
	if status == "" {
		t.Fatal("no status message")
	}
	hookMatePane(t, r.c, "UserPromptSubmit", `{"prompt":"x"}`)
	hookMatePane(t, r.c, "Stop", `{"last_assistant_message":"The query has no index. I added one."}`)
	waitFor(t, func() bool { return len(r.posts()) == 2 })
	if p := r.posts()[1]; p.thread != "1700.0001" || p.text != "The query has no index. I added one." {
		t.Errorf("result %+v", p)
	}
	waitFor(t, func() bool { return len(r.updates()) > 0 })
	ups := r.updates()
	if last := ups[len(ups)-1]; last.thread != status || last.channel != "C0CHAN001" {
		t.Errorf("status update %+v", last)
	}
}

func TestMateWithNoUsableRepoPostsAnErrorAndStartsNothing(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "webby")
	r.hub.send(m, mention(mateGuest, "<@U0BOT0001> please look", "1700.0001"))
	waitFor(t, func() bool { return len(r.posts()) == 1 })
	if p := r.posts()[0]; p.thread != "1700.0001" || !strings.Contains(p.text, "repo") {
		t.Errorf("post %+v", p)
	}
	if s := r.mateSessions(t); len(s) != 0 {
		t.Errorf("sessions %+v", s)
	}
	r.wts.mu.Lock()
	defer r.wts.mu.Unlock()
	if len(r.wts.added) != 0 {
		t.Errorf("worktrees %+v", r.wts.added)
	}
}

func TestMateIgnoresAThreadMessageWithNoLink(t *testing.T) {
	r := startMateRig(t, &memStore{})
	m := r.connect(t, "helper")
	r.hub.send(m, threadReply(app.SlackThreadMessage, mateGuest, "hello", "1700.0005"))
	time.Sleep(50 * time.Millisecond)
	if s := r.mateSessions(t); len(s) != 0 {
		t.Errorf("sessions %+v", s)
	}
	if p := r.posts(); len(p) != 0 {
		t.Errorf("posts %+v", p)
	}
}

func TestMateThreadLinkSurvivesARestart(t *testing.T) {
	store := &memStore{}
	store.snap.Sessions = []domain.Session{{ID: "s1", Harness: domain.HarnessClaude, Pane: "%7", State: domain.StateIdle,
		Mate: &domain.MateSeat{Name: "helper", Owner: mateOwner, Authority: mateGuest}}}
	store.snap.MateThreads = []domain.MateThread{{Team: "T0TEAM001", Channel: "C0CHAN001", ThreadTS: "1700.0001", Mate: "helper", SessionID: "s1", Status: "1800.0001"}}
	r := startMateRig(t, store)
	m := r.connect(t, "helper")
	r.hub.send(m, threadReply(app.SlackThreadMessage, mateGuest, "and the tests", "1700.0009"))
	waitFor(t, func() bool { return r.typed("Gus: and the tests") })
}
