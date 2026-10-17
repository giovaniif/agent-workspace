package domain

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	chainOwner = "UOWNER0001"
	chainGuest = "UGUEST0001"
	chainBotA  = "UBOTA00001"
	chainBotB  = "UBOTB00001"
)

func TestAskedActions(t *testing.T) {
	cases := []struct {
		text string
		want []ActionKind
	}{
		{"please look at the login", nil},
		{"Merge it when the tests pass", []ActionKind{ActionMerge}},
		{"open a PR for this", []ActionKind{ActionCreatePR}},
		{"make a pull request", []ActionKind{ActionCreatePR}},
		{"push the branch", []ActionKind{ActionPush}},
		{"delete the old branch", []ActionKind{ActionDeleteBranch}},
		{"deploy to staging", []ActionKind{ActionDeploy}},
		{"push, then merge, then merge again", []ActionKind{ActionMerge, ActionPush}},
		{"the emergency is over", nil},
		{"the pressure is high", nil},
		{"remove the dead code", nil},
		{"do not merge this PR", nil},
		{"Don't push yet. Deploy it", []ActionKind{ActionDeploy}},
		{"never deploy on friday, but merge it", []ActionKind{ActionMerge}},
		{"no push please", nil},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			if got := AskedActions(c.text); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestDirectMateTurnStartsAChainFromThePerson(t *testing.T) {
	seat := MateSeat{Name: "a", Owner: chainOwner, Authority: chainBotB, Origin: chainBotB, Asked: []ActionKind{ActionPush}, Hops: 4}
	got := DirectMateTurn(seat, chainGuest, "merge it")
	if got.Name != "a" || got.Owner != chainOwner || got.Authority != chainGuest || got.Origin != chainGuest || got.Hops != 0 || !slices.Equal(got.Asked, []ActionKind{ActionMerge}) {
		t.Fatalf("seat %+v", got)
	}
}

func TestChainMateTurnHasTheFirstMateAsAuthorAndOneMoreHop(t *testing.T) {
	from := MateSeat{Name: "a", Owner: chainOwner, Authority: chainOwner, Origin: chainOwner, Asked: []ActionKind{ActionMerge}, Hops: 2}
	got := ChainMateTurn(MateSeat{Name: "b", Owner: chainOwner, Authority: chainGuest, Hops: 9}, from, chainBotA)
	if got.Name != "b" || got.Owner != chainOwner || got.Authority != chainBotA || got.Origin != chainOwner || got.Hops != 3 || !slices.Equal(got.Asked, []ActionKind{ActionMerge}) {
		t.Fatalf("seat %+v", got)
	}
}

func TestMateChainAuthority(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	merge := Action{Kind: ActionMerge, Tool: "Bash", Input: "gh pr merge 42"}
	push := Action{Kind: ActionPush, Tool: "Bash", Input: "git push"}
	read := Action{Kind: ActionSafe, Tool: "Read", Input: "a.go"}
	seatA := MateSeat{Name: "a", Owner: chainOwner}
	seatB := MateSeat{Name: "b", Owner: chainOwner}
	seatC := MateSeat{Name: "c", Owner: chainOwner}
	cases := []struct {
		name   string
		seat   MateSeat
		action Action
		allow  bool
	}{
		{"non-owner chain asking for a merge needs approval", ChainMateTurn(seatB, DirectMateTurn(seatA, chainGuest, "merge it"), chainBotA), merge, false},
		{"owner chain gets the asked merge", ChainMateTurn(seatB, DirectMateTurn(seatA, chainOwner, "merge it"), chainBotA), merge, true},
		{"owner chain does not get a push it did not ask for", ChainMateTurn(seatB, DirectMateTurn(seatA, chainOwner, "merge it"), chainBotA), push, false},
		{"owner chain that asked for nothing dangerous", ChainMateTurn(seatB, DirectMateTurn(seatA, chainOwner, "look at it"), chainBotA), merge, false},
		{"owner chain carries over three mates", ChainMateTurn(seatC, ChainMateTurn(seatB, DirectMateTurn(seatA, chainOwner, "push it"), chainBotA), chainBotB), push, true},
		{"owner of a different mate gives no authority", ChainMateTurn(MateSeat{Name: "b", Owner: "UOTHER0001"}, DirectMateTurn(seatA, chainOwner, "merge it"), chainBotA), merge, false},
		{"a person who is not the owner breaks the chain", ChainMateTurn(seatB, DirectMateTurn(DirectMateTurn(seatA, chainOwner, "merge it"), chainGuest, "merge it now"), chainBotA), merge, false},
		{"a safe action needs no authority", ChainMateTurn(seatB, DirectMateTurn(seatA, chainGuest, "look"), chainBotA), read, true},
		{"the owner in person gets every action", DirectMateTurn(seatA, chainOwner, "look"), push, true},
		{"a mate with no owner gets nothing from the chain", ChainMateTurn(MateSeat{Name: "b"}, DirectMateTurn(MateSeat{Name: "a"}, "", "merge"), chainBotA), merge, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Gate(ChainAuthority(c.seat, c.action.Kind), c.seat.Owner, c.action, nil, now)
			if got.Allow != c.allow {
				t.Fatalf("allow %v, want %v for %+v", got.Allow, c.allow, c.seat)
			}
		})
	}
}

func TestNextMateSeat(t *testing.T) {
	ownerTurn := MateSeat{Owner: chainOwner, Authority: chainOwner, Origin: chainOwner, Hops: 0}
	guestTurn := MateSeat{Owner: chainOwner, Authority: chainGuest, Origin: chainGuest, Hops: 0}
	chainTurn := MateSeat{Owner: chainOwner, Authority: chainBotA, Origin: chainOwner, Asked: []ActionKind{ActionMerge}, Hops: 3}
	cases := []struct {
		name          string
		current, next MateSeat
		busy          bool
		want          MateSeat
	}{
		{"idle takes the next seat", guestTurn, ownerTurn, false, ownerTurn},
		{"busy lowers an owner turn", ownerTurn, chainTurn, true, chainTurn},
		{"busy keeps a guest turn when the owner posts", guestTurn, ownerTurn, true, guestTurn},
		{"busy takes the same author", chainTurn, MateSeat{Owner: chainOwner, Authority: chainBotA, Hops: 5}, true, MateSeat{Owner: chainOwner, Authority: chainBotA, Hops: 5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NextMateSeat(c.current, c.next, c.busy)
			if got.Authority != c.want.Authority || got.Origin != c.want.Origin || got.Hops != c.want.Hops || !slices.Equal(got.Asked, c.want.Asked) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestMateMentions(t *testing.T) {
	if !MateMentions("hey <@UBOTB00001> your turn", chainBotB) {
		t.Error("a mention is not seen")
	}
	if MateMentions("hey <@UBOTB00001> your turn", chainBotA) {
		t.Error("a mention of a different user counts")
	}
	if MateMentions("hey UBOTA00001", chainBotA) {
		t.Error("a bare ID counts as a mention")
	}
	if MateMentions("hey <@>", "") {
		t.Error("an empty user counts")
	}
}

func TestDefaultMateLimits(t *testing.T) {
	l := DefaultMateLimits()
	if l.HopLimit != 5 || l.TurnsPerHour != 20 || l.DailyTurns != 100 || l.DailyTokens != 0 || l.Stop {
		t.Fatalf("defaults %+v", l)
	}
}

func usageWith(now time.Time, ago ...time.Duration) MateUsage {
	var u MateUsage
	for _, a := range ago {
		u = u.AddTurn(now.Add(-a))
	}
	return u
}

func TestMateGuard(t *testing.T) {
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	limits := MateLimits{HopLimit: 5, TurnsPerHour: 3, DailyTurns: 5, DailyTokens: 1000}
	stopped := limits
	stopped.Stop = true
	off := MateLimits{}
	cases := []struct {
		name   string
		limits MateLimits
		hops   int
		usage  MateUsage
		want   MateGuardStop
	}{
		{"nothing used", limits, 0, MateUsage{}, GuardNone},
		{"global stop", stopped, 0, MateUsage{}, GuardStopped},
		{"hop limit reached is still fine", limits, 5, MateUsage{}, GuardNone},
		{"hop limit passed", limits, 6, MateUsage{}, GuardHops},
		{"two turns in the hour", limits, 0, usageWith(now, time.Minute, 2*time.Minute), GuardNone},
		{"three turns in the hour", limits, 0, usageWith(now, time.Minute, 2*time.Minute, 59*time.Minute), GuardRate},
		{"old turns do not count for the hour", limits, 0, usageWith(now, time.Minute, 2*time.Minute, time.Hour), GuardNone},
		{"daily turns", limits, 0, usageWith(now, 2*time.Hour, 3*time.Hour, 4*time.Hour, 5*time.Hour, 6*time.Hour), GuardDailyTurns},
		{"yesterday does not count", limits, 0, usageWith(now, 2*time.Hour, 3*time.Hour, 4*time.Hour, 5*time.Hour, 16*time.Hour), GuardNone},
		{"daily tokens", limits, 0, MateUsage{}.AddTokens(600, now).AddTokens(400, now), GuardDailyTokens},
		{"tokens of yesterday", limits, 0, MateUsage{}.AddTokens(5000, now.Add(-16*time.Hour)).AddTokens(10, now), GuardNone},
		{"zero turns off every limit", off, 99, usageWith(now, time.Minute, time.Minute, time.Minute).AddTokens(1e9, now), GuardNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MateGuard(c.limits, c.hops, c.usage, now); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMateUsageKeepsOnlyTheLastDay(t *testing.T) {
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	u := usageWith(now, 30*time.Hour, 25*time.Hour, time.Hour)
	if len(u.Turns) != 1 {
		t.Fatalf("turns %v", u.Turns)
	}
}

func TestMateGuardTextSaysWhy(t *testing.T) {
	l := MateLimits{HopLimit: 5, TurnsPerHour: 20, DailyTurns: 100, DailyTokens: 50000}
	cases := map[MateGuardStop]string{
		GuardStopped:     "stopped",
		GuardHops:        "5 hops",
		GuardRate:        "20 turns in one hour",
		GuardDailyTurns:  "100 turns",
		GuardDailyTokens: "50000 tokens",
	}
	for stop, want := range cases {
		if got := MateGuardText(stop, l); !strings.Contains(got, want) {
			t.Errorf("%s: %q misses %q", stop, got, want)
		}
	}
	if MateGuardText(GuardNone, l) != "" {
		t.Error("no stop has a text")
	}
}
