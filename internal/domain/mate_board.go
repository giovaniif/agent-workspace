package domain

import (
	"net/url"
	"slices"
	"strings"
	"time"
)

type MateStatus struct {
	Name  string         `json:"name"`
	State MateConnection `json:"state"`
	Error string         `json:"error,omitempty"`
	Guard MateGuardStop  `json:"guard,omitempty"`
}

type MateBoard struct {
	Stopped bool         `json:"stopped"`
	Mates   []MateStatus `json:"mates"`
}

type MateLink struct {
	Mate      string `json:"mate"`
	Team      string `json:"team"`
	Channel   string `json:"channel"`
	ThreadTS  string `json:"thread_ts"`
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

func NewMateBoard(states []MateStatus, limits MateLimits, usage map[string]MateUsage, now time.Time) MateBoard {
	board := MateBoard{Stopped: limits.Stop, Mates: make([]MateStatus, 0, len(states))}
	for _, s := range states {
		s.Guard = MateGuard(limits, 0, usage[s.Name], now)
		board.Mates = append(board.Mates, s)
	}
	slices.SortFunc(board.Mates, func(a, b MateStatus) int { return strings.Compare(a.Name, b.Name) })
	return board
}

func (t MateThread) Link() MateLink {
	return MateLink{
		Mate: t.Mate, Team: t.Team, Channel: t.Channel, ThreadTS: t.ThreadTS, SessionID: t.SessionID,
		URL: MateThreadURL(t.Team, t.Channel, t.ThreadTS),
	}
}

func MateThreadURL(team, channel, threadTS string) string {
	if team == "" || channel == "" || threadTS == "" {
		return ""
	}
	return "https://app.slack.com/client/" + url.PathEscape(team) + "/" + url.PathEscape(channel) + "/thread/" + url.PathEscape(channel+"-"+threadTS)
}

func MateLinks(threads []MateThread) []MateLink {
	out := make([]MateLink, 0, len(threads))
	for _, t := range threads {
		out = append(out, t.Link())
	}
	slices.SortFunc(out, func(a, b MateLink) int {
		return strings.Compare(a.Mate+"\x00"+a.ThreadTS+"\x00"+a.Channel, b.Mate+"\x00"+b.ThreadTS+"\x00"+b.Channel)
	})
	return out
}

func MateWorking(s AgentState) bool {
	return s == StateRunning || s == StatePermission || s == StateWaiting
}
