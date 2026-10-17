package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var askedWords = []struct {
	kind ActionKind
	re   *regexp.Regexp
}{
	{ActionMerge, regexp.MustCompile(`(?i)\bmerge\b`)},
	{ActionCreatePR, regexp.MustCompile(`(?i)\b(pr|pull request)\b`)},
	{ActionPush, regexp.MustCompile(`(?i)\bpush\b`)},
	{ActionDeleteBranch, regexp.MustCompile(`(?i)\bdelete\b.*\bbranch\b`)},
	{ActionDeploy, regexp.MustCompile(`(?i)\bdeploy\b`)},
}

func AskedActions(text string) []ActionKind {
	var out []ActionKind
	for _, w := range askedWords {
		if w.re.MatchString(text) {
			out = append(out, w.kind)
		}
	}
	return out
}

func DirectMateTurn(seat MateSeat, user, text string) MateSeat {
	seat.Authority = user
	seat.Origin = user
	seat.Asked = AskedActions(text)
	seat.Hops = 0
	return seat
}

func ChainMateTurn(seat, from MateSeat, author string) MateSeat {
	seat.Authority = author
	seat.Origin = from.Origin
	seat.Asked = from.Asked
	seat.Hops = from.Hops + 1
	return seat
}

func ChainAuthority(seat MateSeat, kind ActionKind) string {
	if seat.Owner != "" && seat.Origin == seat.Owner && slices.Contains(seat.Asked, kind) {
		return seat.Owner
	}
	return seat.Authority
}

func NextMateSeat(current, next MateSeat, busy bool) MateSeat {
	if NextMateAuthority(current.Authority, next.Authority, next.Owner, busy) == next.Authority {
		return next
	}
	return current
}

func MateMentions(text, user string) bool {
	return user != "" && strings.Contains(text, "<@"+user+">")
}

type MateLimits struct {
	HopLimit     int
	TurnsPerHour int
	DailyTurns   int
	DailyTokens  int64
	Stop         bool
}

func DefaultMateLimits() MateLimits {
	return MateLimits{HopLimit: 5, TurnsPerHour: 20, DailyTurns: 100}
}

type MateGuardStop string

const (
	GuardNone        MateGuardStop = ""
	GuardStopped     MateGuardStop = "stopped"
	GuardHops        MateGuardStop = "hop_limit"
	GuardRate        MateGuardStop = "rate_limit"
	GuardDailyTurns  MateGuardStop = "daily_turns"
	GuardDailyTokens MateGuardStop = "daily_tokens"
)

type MateUsage struct {
	Turns     []time.Time
	Tokens    int64
	TokensDay string
}

func usageDay(t time.Time) string {
	return t.Format(time.DateOnly)
}

func (u MateUsage) AddTurn(now time.Time) MateUsage {
	kept := []time.Time{}
	for _, t := range u.Turns {
		if t.After(now.Add(-24 * time.Hour)) {
			kept = append(kept, t)
		}
	}
	u.Turns = append(kept, now)
	return u
}

func (u MateUsage) AddTokens(n int64, now time.Time) MateUsage {
	if day := usageDay(now); u.TokensDay != day {
		u.TokensDay, u.Tokens = day, 0
	}
	u.Tokens += n
	return u
}

func (u MateUsage) tokensOn(now time.Time) int64 {
	if u.TokensDay != usageDay(now) {
		return 0
	}
	return u.Tokens
}

func (u MateUsage) turnsAfter(from time.Time) int {
	n := 0
	for _, t := range u.Turns {
		if t.After(from) {
			n++
		}
	}
	return n
}

func MateGuard(l MateLimits, hops int, u MateUsage, now time.Time) MateGuardStop {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(-time.Nanosecond)
	switch {
	case l.Stop:
		return GuardStopped
	case l.HopLimit > 0 && hops > l.HopLimit:
		return GuardHops
	case l.DailyTokens > 0 && u.tokensOn(now) >= l.DailyTokens:
		return GuardDailyTokens
	case l.DailyTurns > 0 && u.turnsAfter(dayStart) >= l.DailyTurns:
		return GuardDailyTurns
	case l.TurnsPerHour > 0 && u.turnsAfter(now.Add(-time.Hour)) >= l.TurnsPerHour:
		return GuardRate
	}
	return GuardNone
}

func MateGuardText(stop MateGuardStop, l MateLimits) string {
	switch stop {
	case GuardStopped:
		return "All mates are stopped by the owner. I did not start this turn."
	case GuardHops:
		return fmt.Sprintf("This thread passed the limit of %d hops from mate to mate. I stopped so that the mates do not loop. A person must post in the thread to continue.", l.HopLimit)
	case GuardRate:
		return fmt.Sprintf("I did my limit of %d turns in one hour. I did not start this turn. Try again later.", l.TurnsPerHour)
	case GuardDailyTurns:
		return fmt.Sprintf("I did my daily limit of %d turns. I did not start this turn. Try again tomorrow.", l.DailyTurns)
	case GuardDailyTokens:
		return fmt.Sprintf("I used my daily limit of %d tokens. I did not start this turn. Try again tomorrow.", l.DailyTokens)
	}
	return ""
}
