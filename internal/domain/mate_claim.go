package domain

import (
	"strings"
	"time"
)

type MateConnection string

const (
	MateConnecting MateConnection = "connecting"
	MateConnected  MateConnection = "connected"
	MateError      MateConnection = "error"
	MatePaused     MateConnection = "paused"
	MateNoTokens   MateConnection = "no_tokens"
)

const MateClaimTTL = 10 * time.Minute

type MateClaim struct {
	Mate    string
	Code    string
	Expires time.Time
}

func NewMateClaim(mate, code string, now time.Time) MateClaim {
	return MateClaim{Mate: mate, Code: code, Expires: now.Add(MateClaimTTL)}
}

func (c MateClaim) Accepts(user, text string, now time.Time) bool {
	return c.Code != "" &&
		now.Before(c.Expires) &&
		slackUserID.MatchString(user) &&
		NormalizePairCode(strings.TrimSpace(text)) == c.Code
}
