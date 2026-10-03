package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	LowQuotaLeft    = 20
	WarnQuotaLeft   = 25
	StaleQuotaAfter = 15 * time.Minute
)

type Quota struct {
	Harness     Harness
	Window      string
	LeftPercent int
	ResetsAt    int64
	ReportedAt  time.Time
}

func (q Quota) Low() bool { return q.LeftPercent < LowQuotaLeft }

func (q Quota) Age(now time.Time) time.Duration { return max(now.Sub(q.ReportedAt), 0) }

func (q Quota) Stale(now time.Time) bool { return q.Age(now) > StaleQuotaAfter }

// why: accounts are shared by every session of a harness, so the newest
// report is the best guess.
func Quotas(sessions []Session) []Quota {
	type key struct {
		harness Harness
		window  string
	}
	best := map[key]Quota{}
	for _, s := range sessions {
		for _, l := range s.Limits {
			k := key{s.Harness, l.Window}
			if old, ok := best[k]; ok && old.ReportedAt.After(s.LimitsAt) {
				continue
			}
			best[k] = Quota{
				Harness:     s.Harness,
				Window:      l.Window,
				LeftPercent: min(max(100-l.UsedPercent, 0), 100),
				ResetsAt:    l.ResetsAt,
				ReportedAt:  s.LimitsAt,
			}
		}
	}
	out := make([]Quota, 0, len(best))
	for _, q := range best {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Harness != b.Harness {
			return harnessRank(a.Harness) < harnessRank(b.Harness)
		}
		if da, db := windowDuration(a.Window), windowDuration(b.Window); da != db {
			return da < db
		}
		return a.Window < b.Window
	})
	return out
}

// why: a window whose reset has passed is over, and Claude Code stops
// reporting it then too.
func Current(quotas []Quota, now time.Time) []Quota {
	out := make([]Quota, 0, len(quotas))
	for _, q := range quotas {
		if q.ResetsAt != 0 && q.ResetsAt <= now.Unix() {
			continue
		}
		out = append(out, q)
	}
	return out
}

func harnessRank(h Harness) int {
	for i, spec := range harnessTable {
		if spec.Harness == h {
			return i
		}
	}
	return len(harnessTable)
}

const (
	fiveHours = 5 * time.Hour
	sevenDays = 7 * 24 * time.Hour
)

func windowDuration(window string) time.Duration {
	switch {
	case window == "five_hour":
		return fiveHours
	case strings.HasPrefix(window, "seven_day"):
		return sevenDays
	}
	var minutes int
	if _, err := fmt.Sscanf(window, "%dm", &minutes); err == nil && strings.HasSuffix(window, "m") {
		return time.Duration(minutes) * time.Minute
	}
	return 1<<63 - 1
}

func WindowNameForMinutes(minutes int) string {
	switch time.Duration(minutes) * time.Minute {
	case fiveHours:
		return "five_hour"
	case sevenDays:
		return "seven_day"
	}
	return fmt.Sprintf("%dm", minutes)
}

func WindowLabel(window string) string {
	switch {
	case window == "five_hour":
		return "5h"
	case window == "seven_day":
		return "7d"
	case strings.HasPrefix(window, "seven_day_"):
		return "7d " + strings.TrimPrefix(window, "seven_day_")
	}
	d := windowDuration(window)
	if d == 1<<63-1 {
		return window
	}
	minutes := int(d / time.Minute)
	switch {
	case minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	case minutes > 60:
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}

type SwitchAdvice struct {
	Low           Quota
	Other         Harness
	OtherShortest *Quota
}

func Advise(quotas []Quota, chosen Harness) (SwitchAdvice, bool) {
	return AdviseAt(quotas, chosen, WarnQuotaLeft)
}

func AdviseAt(quotas []Quota, chosen Harness, threshold int) (SwitchAdvice, bool) {
	low, ok := ShortestQuota(quotas, chosen)
	if !ok || low.LeftPercent >= threshold {
		return SwitchAdvice{}, false
	}
	advice := SwitchAdvice{Low: low}
	for _, h := range Harnesses() {
		if h == chosen {
			continue
		}
		if advice.Other == "" {
			advice.Other = h
		}
		q, ok := ShortestQuota(quotas, h)
		if ok && (advice.OtherShortest == nil || q.LeftPercent > advice.OtherShortest.LeftPercent) {
			advice.Other, advice.OtherShortest = h, &q
		}
	}
	return advice, true
}

func ShortestQuota(quotas []Quota, h Harness) (Quota, bool) {
	var best Quota
	found := false
	for _, q := range quotas {
		if q.Harness != h {
			continue
		}
		if !found || windowDuration(q.Window) < windowDuration(best.Window) ||
			windowDuration(q.Window) == windowDuration(best.Window) && q.LeftPercent < best.LeftPercent {
			best, found = q, true
		}
	}
	return best, found
}
