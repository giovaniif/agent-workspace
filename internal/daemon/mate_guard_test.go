package daemon_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const (
	helperBot  = "xoxb-111-222-helperbot"
	webbyBot   = "xoxb-111-222-webbybot"
	helperUser = "U0BOTHELP1"
	webbyUser  = "U0BOTWEB01"
)

const pairMates = `
[[mate]]
name = "helper"
prompt = "You help with the api."
agent = "claude"
repos = ["api"]
owner = "U0OWNER01"

[[mate]]
name = "webby"
prompt = "You help too."
agent = "claude"
repos = ["api"]
owner = "U0OWNER01"
`

type matePair struct {
	mateRig
	helper, webby *fakeSlackMate
}

func startMatePair(t *testing.T, limits string) matePair {
	t.Helper()
	r := startMateRigWith(t, &memStore{}, limits+pairMates)
	r.hub.mu.Lock()
	r.hub.users = map[string]string{helperBot: helperUser, webbyBot: webbyUser}
	r.hub.names[helperUser] = "helper"
	r.hub.names[webbyUser] = "webby"
	r.hub.mu.Unlock()
	if _, err := setTokens(r.c, "helper", helperBot, knownApp); err != nil {
		t.Fatal(err)
	}
	helper := r.hub.liveBot(t, helperBot)
	if _, err := setTokens(r.c, "webby", webbyBot, knownApp); err != nil {
		t.Fatal(err)
	}
	webby := r.hub.liveBot(t, webbyBot)
	waitFor(t, func() bool {
		var list rpc.MateList
		if err := r.c.Call(context.Background(), rpc.MethodMateList, nil, &list); err != nil {
			return false
		}
		connected := 0
		for _, s := range list.States {
			if s.State == domain.MateConnected {
				connected++
			}
		}
		return connected == 2
	})
	return matePair{mateRig: r, helper: helper, webby: webby}
}

func (p matePair) seatOf(t *testing.T, name string) *domain.MateSeat {
	t.Helper()
	for _, s := range p.mateSessions(t) {
		if s.Mate.Name == name {
			return s.Mate
		}
	}
	return nil
}

func (p matePair) linked(t *testing.T, name string) bool {
	t.Helper()
	for _, l := range p.store.mateThreads() {
		if l.Mate == name {
			return true
		}
	}
	return false
}

func guardPosts(posts []fakeSlackPost, want string) int {
	n := 0
	for _, p := range posts {
		if strings.Contains(p.text, want) {
			n++
		}
	}
	return n
}

func TestMateMentionOfAMateStartsATurnWithTheFirstMateAsAuthor(t *testing.T) {
	p := startMatePair(t, "")
	p.hub.send(p.helper, mention(mateOwner, "<@U0BOTHELP1> merge it when it is green", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
	p.hub.send(p.webby, threadReply(app.SlackMention, helperUser, "<@U0BOTWEB01> please merge the PR", "1700.0002"))
	var seat *domain.MateSeat
	waitFor(t, func() bool { seat = p.seatOf(t, "webby"); return seat != nil })
	if seat.Authority != helperUser || seat.Origin != mateOwner || seat.Hops != 1 || !slices.Contains(seat.Asked, domain.ActionMerge) {
		t.Fatalf("seat %+v", seat)
	}
	waitFor(t, func() bool {
		for _, q := range queuedTexts(t, p.c) {
			if strings.Contains(q, "Asked by: helper") {
				return true
			}
		}
		return false
	})
}

func TestMateThreadMessageOfAMateWithNoMentionIsIgnored(t *testing.T) {
	p := startMatePair(t, "")
	p.hub.send(p.webby, mention(mateGuest, "<@U0BOTWEB01> look", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "webby") })
	before := len(queuedTexts(t, p.c))
	p.hub.send(p.webby, threadReply(app.SlackThreadMessage, helperUser, "Working on it.", "1700.0003"))
	time.Sleep(50 * time.Millisecond)
	if got := len(queuedTexts(t, p.c)); got != before {
		t.Fatalf("queued %d, want %d", got, before)
	}
}

func TestTwoMatePingPongStopsAtTheHopLimit(t *testing.T) {
	p := startMatePair(t, "")
	p.hub.send(p.helper, mention(mateOwner, "<@U0BOTHELP1> talk to webby", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
	type hop struct {
		to     *fakeSlackMate
		from   string
		toName string
		text   string
	}
	toWebby := hop{p.webby, helperUser, "webby", "<@U0BOTWEB01> your turn"}
	toHelper := hop{p.helper, webbyUser, "helper", "<@U0BOTHELP1> your turn"}
	for i := 1; i <= 5; i++ {
		h := toWebby
		if i%2 == 0 {
			h = toHelper
		}
		p.hub.send(h.to, threadReply(app.SlackMention, h.from, h.text, fmt.Sprintf("1700.%04d", 10+i)))
		want := i
		waitFor(t, func() bool { s := p.seatOf(t, h.toName); return s != nil && s.Hops == want })
	}
	p.hub.send(p.helper, threadReply(app.SlackMention, webbyUser, "<@U0BOTHELP1> your turn", "1700.0090"))
	waitFor(t, func() bool { return guardPosts(p.hub.postsOf(p.helper), "limit of 5 hops") == 1 })
	p.hub.send(p.helper, threadReply(app.SlackMention, webbyUser, "<@U0BOTHELP1> again", "1700.0091"))
	time.Sleep(50 * time.Millisecond)
	if n := guardPosts(p.hub.postsOf(p.helper), "hops"); n != 1 {
		t.Fatalf("%d hop messages, want 1", n)
	}
	if s := p.seatOf(t, "helper"); s.Hops != 4 {
		t.Fatalf("helper seat %+v", s)
	}
}

func TestMateRateLimitStopsTheTurnAndSaysWhyOneTime(t *testing.T) {
	p := startMatePair(t, "[mates]\nturns_per_hour = 1\n")
	p.hub.send(p.helper, mention(mateGuest, "<@U0BOTHELP1> look", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
	before := len(queuedTexts(t, p.c))
	p.hub.send(p.helper, threadReply(app.SlackThreadMessage, mateGuest, "and more", "1700.0002"))
	waitFor(t, func() bool { return guardPosts(p.hub.postsOf(p.helper), "1 turns in one hour") == 1 })
	p.hub.send(p.helper, threadReply(app.SlackThreadMessage, mateGuest, "hello?", "1700.0003"))
	time.Sleep(50 * time.Millisecond)
	if n := guardPosts(p.hub.postsOf(p.helper), "in one hour"); n != 1 {
		t.Fatalf("%d rate messages, want 1", n)
	}
	if got := len(queuedTexts(t, p.c)); got != before {
		t.Fatalf("queued %d, want %d", got, before)
	}
	var list rpc.MateList
	if err := p.c.Call(context.Background(), rpc.MethodMateList, nil, &list); err != nil {
		t.Fatal(err)
	}
	if list.Limits.TurnsPerHour != 1 || list.Limits.HopLimit != 5 {
		t.Errorf("limits %+v", list.Limits)
	}
	for _, s := range list.States {
		if want := map[string]domain.MateGuardStop{"helper": domain.GuardRate, "webby": domain.GuardNone}[s.Name]; s.Guard != want {
			t.Errorf("%s guard %q, want %q", s.Name, s.Guard, want)
		}
	}
}

func TestMateGlobalStopStartsNothing(t *testing.T) {
	p := startMatePair(t, "[mates]\nstop = true\n")
	p.hub.send(p.helper, mention(mateOwner, "<@U0BOTHELP1> look", "1700.0001"))
	waitFor(t, func() bool { return guardPosts(p.hub.postsOf(p.helper), "stopped") == 1 })
	if s := p.mateSessions(t); len(s) != 0 {
		t.Fatalf("sessions %+v", s)
	}
}

func TestMateDailyTokenBudgetStopsTheNextTurn(t *testing.T) {
	p := startMatePair(t, "[mates]\ndaily_tokens = 1000\n")
	p.hub.send(p.helper, mention(mateGuest, "<@U0BOTHELP1> look", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
	if err := p.c.Call(context.Background(), rpc.MethodStatusLine, rpc.StatusLine{Pane: "%7", Report: domain.StatusReport{Tokens: 1500}}, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { s := p.mateSessions(t); return len(s) == 1 && s[0].Usage.Tokens == 1500 })
	p.hub.send(p.helper, threadReply(app.SlackThreadMessage, mateGuest, "and more", "1700.0002"))
	waitFor(t, func() bool { return guardPosts(p.hub.postsOf(p.helper), "1000 tokens") == 1 })
}

func TestMateLimitsWithAnUnknownKeyIsAProblem(t *testing.T) {
	p := startMatePair(t, "[mates]\nhops = 3\n")
	var list rpc.MateList
	if err := p.c.Call(context.Background(), rpc.MethodMateList, nil, &list); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(list.Problems, "\n"), "mates.hops") {
		t.Fatalf("problems %q", list.Problems)
	}
}
