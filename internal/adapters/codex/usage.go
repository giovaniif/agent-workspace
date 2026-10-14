package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const (
	UnknownPercent = -1

	promptBaselineTokens = 12000
)

type LimitWindow struct {
	WindowMinutes int
	UsedPercent   float64
	ResetsAt      time.Time
}

type Snapshot struct {
	Model              string
	Effort             string
	ContextLeftPercent int
	Limits             []LimitWindow
	LimitsAt           time.Time
	TurnAt             time.Time
}

func (s Snapshot) RateLimits() []domain.RateLimit {
	var out []domain.RateLimit
	for _, w := range s.Limits {
		out = append(out, domain.RateLimit{
			Window:      domain.WindowNameForMinutes(w.WindowMinutes),
			UsedPercent: int(math.Round(w.UsedPercent)),
			ResetsAt:    w.ResetsAt.Unix(),
		})
	}
	return out
}

func (s Snapshot) Usage() domain.Usage {
	var highest float64
	for _, w := range s.Limits {
		highest = math.Max(highest, w.UsedPercent)
	}
	return domain.Usage{
		ContextLeftPercent: s.ContextLeftPercent,
		LimitUsedPercent:   int(math.Round(highest)),
	}
}

func ContextLeftPercent(usedTokens, window int) int {
	effective := window - promptBaselineTokens
	if effective <= 0 {
		return 0
	}
	used := max(usedTokens-promptBaselineTokens, 0)
	left := max(effective-used, 0)
	return int(math.Round(float64(left) / float64(effective) * 100))
}

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type turnContext struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type eventMsg struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info"`
	RateLimits *rateLimits `json:"rate_limits"`
}

type tokenInfo struct {
	Last struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"last_token_usage"`
	ContextWindow int `json:"model_context_window"`
}

type rateLimits struct {
	Primary   *rateWindow `json:"primary"`
	Secondary *rateWindow `json:"secondary"`
}

type rateWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func ReadSnapshot(r io.Reader) (Snapshot, error) {
	snap := Snapshot{ContextLeftPercent: UnknownPercent}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var line rolloutLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "turn_context":
			var tc turnContext
			if json.Unmarshal(line.Payload, &tc) == nil {
				snap.Model = orKeep(tc.Model, snap.Model)
				snap.Effort = orKeep(tc.Effort, snap.Effort)
				snap.TurnAt, _ = time.Parse(time.RFC3339Nano, line.Timestamp)
			}
		case "event_msg":
			var ev eventMsg
			if json.Unmarshal(line.Payload, &ev) == nil && ev.Type == "token_count" {
				at, _ := time.Parse(time.RFC3339Nano, line.Timestamp)
				snap.applyTokenCount(ev, at)
			}
		}
	}
	return snap, sc.Err()
}

func orKeep(next, current string) string {
	if next == "" {
		return current
	}
	return next
}

func (s *Snapshot) applyTokenCount(ev eventMsg, at time.Time) {
	if ev.Info != nil && ev.Info.ContextWindow > 0 {
		s.ContextLeftPercent = ContextLeftPercent(ev.Info.Last.TotalTokens, ev.Info.ContextWindow)
	}
	if ev.RateLimits == nil {
		return
	}
	s.Limits = nil
	s.LimitsAt = at
	for _, w := range []*rateWindow{ev.RateLimits.Primary, ev.RateLimits.Secondary} {
		if w != nil {
			s.Limits = append(s.Limits, LimitWindow{
				WindowMinutes: w.WindowMinutes,
				UsedPercent:   w.UsedPercent,
				ResetsAt:      time.Unix(w.ResetsAt, 0),
			})
		}
	}
}

func ReadSnapshotFile(path string, maxBytes int64) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	if info.Size() > maxBytes {
		if _, err := f.Seek(-maxBytes, io.SeekEnd); err != nil {
			return Snapshot{}, err
		}
		tail, err := io.ReadAll(f)
		if err != nil {
			return Snapshot{}, err
		}
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
		return ReadSnapshot(bytes.NewReader(tail))
	}
	return ReadSnapshot(f)
}
