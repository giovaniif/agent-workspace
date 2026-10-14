package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ScheduleWait   = "wait"
	ScheduleRun    = "run"
	ScheduleMissed = "missed"

	DefaultMissedGrace = 15 * time.Minute

	scheduleSearchDays = 366 * 8
	scheduleMaxSteps   = 100000
)

type Schedule struct {
	minutes [60]bool
	hours   [24]bool
	doms    [32]bool
	months  [13]bool
	dows    [7]bool
	domAll  bool
	dowAll  bool
}

var dayNames = map[string]int{
	"sun": 0, "sunday": 0, "mon": 1, "monday": 1, "tue": 2, "tuesday": 2, "wed": 3, "wednesday": 3,
	"thu": 4, "thursday": 4, "fri": 5, "friday": 5, "sat": 6, "saturday": 6,
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

func ParseSchedule(text string) (Schedule, error) {
	fields := strings.Fields(strings.ToLower(text))
	bad := func() (Schedule, error) {
		return Schedule{}, fmt.Errorf("schedule %q must be hourly, daily HH:MM, weekdays HH:MM, weekly <day> HH:MM or a cron expression", text)
	}
	if len(fields) == 0 {
		return bad()
	}
	switch fields[0] {
	case "hourly":
		if len(fields) != 1 {
			return bad()
		}
		return parseCron([]string{"0", "*", "*", "*", "*"})
	case "daily", "weekdays":
		if len(fields) != 2 {
			return bad()
		}
		h, m, ok := clockTime(fields[1])
		if !ok {
			return bad()
		}
		dow := "*"
		if fields[0] == "weekdays" {
			dow = "1-5"
		}
		return parseCron([]string{m, h, "*", "*", dow})
	case "weekly":
		if len(fields) != 3 {
			return bad()
		}
		day, found := dayNames[fields[1]]
		h, m, ok := clockTime(fields[2])
		if !found || !ok {
			return bad()
		}
		return parseCron([]string{m, h, "*", "*", strconv.Itoa(day)})
	}
	if len(fields) != 5 {
		return bad()
	}
	s, err := parseCron(fields)
	if err != nil {
		return Schedule{}, fmt.Errorf("schedule %q: %w", text, err)
	}
	return s, nil
}

func clockTime(s string) (string, string, bool) {
	h, m, found := strings.Cut(s, ":")
	if !found || len(m) != 2 || len(h) < 1 || len(h) > 2 {
		return "", "", false
	}
	hv, err1 := strconv.Atoi(h)
	mv, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hv < 0 || hv > 23 || mv < 0 || mv > 59 {
		return "", "", false
	}
	return strconv.Itoa(hv), strconv.Itoa(mv), true
}

func parseCron(f []string) (Schedule, error) {
	var s Schedule
	if err := cronField(f[0], 0, 59, nil, s.minutes[:]); err != nil {
		return s, fmt.Errorf("minute: %w", err)
	}
	if err := cronField(f[1], 0, 23, nil, s.hours[:]); err != nil {
		return s, fmt.Errorf("hour: %w", err)
	}
	if err := cronField(f[2], 1, 31, nil, s.doms[:]); err != nil {
		return s, fmt.Errorf("day of month: %w", err)
	}
	if err := cronField(f[3], 1, 12, monthNames, s.months[:]); err != nil {
		return s, fmt.Errorf("month: %w", err)
	}
	var dows [8]bool
	if err := cronField(f[4], 0, 7, dayNames, dows[:]); err != nil {
		return s, fmt.Errorf("day of week: %w", err)
	}
	copy(s.dows[:], dows[:7])
	s.dows[0] = s.dows[0] || dows[7]
	s.domAll = strings.HasPrefix(f[2], "*")
	s.dowAll = strings.HasPrefix(f[4], "*")
	return s, nil
}

func cronField(field string, lo, hi int, names map[string]int, set []bool) error {
	value := func(v string) (int, error) {
		if n, found := names[v]; found {
			return n, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return 0, fmt.Errorf("%q is not in %d-%d", v, lo, hi)
		}
		return n, nil
	}
	for _, part := range strings.Split(field, ",") {
		rng, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return fmt.Errorf("step %q", stepText)
			}
			step = n
		}
		from, to := lo, hi
		if rng != "*" {
			a, b, isRange := strings.Cut(rng, "-")
			var err error
			if from, err = value(a); err != nil {
				return err
			}
			to = from
			if isRange {
				if to, err = value(b); err != nil {
					return err
				}
			} else if hasStep {
				to = hi
			}
			if to < from {
				return fmt.Errorf("range %q runs backwards", rng)
			}
		}
		for v := from; v <= to; v += step {
			set[v] = true
		}
	}
	return nil
}

func (s Schedule) dayMatches(day time.Time) bool {
	if !s.months[day.Month()] {
		return false
	}
	dom, dow := s.doms[day.Day()], s.dows[day.Weekday()]
	if s.domAll || s.dowAll {
		return dom && dow
	}
	return dom || dow
}

func (s Schedule) Next(after time.Time, loc *time.Location) time.Time {
	local := after.In(loc)
	y, m, d := local.Date()
	for i := 0; i < scheduleSearchDays; i++ {
		day := time.Date(y, m, d+i, 12, 0, 0, 0, loc)
		if !s.dayMatches(day) {
			continue
		}
		for h := 0; h < 24; h++ {
			if !s.hours[h] {
				continue
			}
			for mi := 0; mi < 60; mi++ {
				if !s.minutes[mi] {
					continue
				}
				cand := wallTime(day, h, mi, loc)
				if cand.After(after) {
					return cand
				}
			}
		}
	}
	return time.Time{}
}

func ScheduleDecision(s Schedule, loc *time.Location, since, now time.Time, grace time.Duration) (string, time.Time) {
	due := s.Next(since, loc)
	if due.IsZero() || due.After(now) {
		return ScheduleWait, due
	}
	for i := 0; i < scheduleMaxSteps; i++ {
		next := s.Next(due, loc)
		if next.IsZero() || next.After(now) {
			break
		}
		due = next
	}
	if now.Sub(due) <= grace {
		return ScheduleRun, due
	}
	return ScheduleMissed, due
}

func AutomationHash(a Automation) string {
	a.Enabled, a.Trusted = false, false
	data, _ := json.Marshal(a)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func wallTime(day time.Time, h, m int, loc *time.Location) time.Time {
	y, mo, d := day.Date()
	t := time.Date(y, mo, d, h, m, 0, 0, loc)
	if t.Hour() == h && t.Minute() == m {
		return t
	}
	_, before := time.Date(y, mo, d, 0, 0, 0, 0, loc).Zone()
	return time.Date(y, mo, d, h, m, 0, 0, time.UTC).Add(-time.Duration(before) * time.Second).In(loc)
}

type AutomationTrust struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

func AutomationTrustKey(a Automation) string {
	return a.Source + "\x00" + a.Name
}

func AutomationNeedsTrust(a Automation) bool {
	return filepath.Base(a.Source) == ".agentws.toml" && (a.Schedule != "" || len(a.On) > 0 || a.Precheck != "")
}

func AutomationCanRunAlone(a Automation, trusted map[string]string) bool {
	return a.Enabled && (!AutomationNeedsTrust(a) || trusted[AutomationTrustKey(a)] == AutomationHash(a))
}
