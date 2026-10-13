package domain

import (
	"strings"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no tzdata for %s: %v", name, err)
	}
	return loc
}

func TestParseScheduleRefusesBadForms(t *testing.T) {
	for _, in := range []string{
		"", "often", "daily", "daily 25:00", "daily 9:60", "daily 9", "weekdays", "weekly 09:00",
		"weekly funday 09:00", "hourly 3", "* * * *", "61 * * * *", "* 24 * * *", "* * 0 * *",
		"* * * 13 *", "* * * * 8", "*/0 * * * *", "5-1 * * * *", "a * * * *",
	} {
		if _, err := ParseSchedule(in); err == nil {
			t.Errorf("ParseSchedule(%q) = nil error", in)
		}
	}
}

func TestNextRun(t *testing.T) {
	utc := time.UTC
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, utc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		sched string
		after string
		want  string
	}{
		{"hourly", "2026-10-13 09:15", "2026-10-13 10:00"},
		{"hourly", "2026-10-13 09:00", "2026-10-13 10:00"},
		{"daily 09:30", "2026-10-13 09:00", "2026-10-13 09:30"},
		{"daily 09:30", "2026-10-13 09:30", "2026-10-14 09:30"},
		{"daily 9:05", "2026-10-13 23:00", "2026-10-14 09:05"},
		{"weekdays 08:00", "2026-10-16 09:00", "2026-10-19 08:00"},
		{"weekdays 08:00", "2026-10-17 09:00", "2026-10-19 08:00"},
		{"weekdays 08:00", "2026-10-13 07:00", "2026-10-13 08:00"},
		{"weekly mon 09:00", "2026-10-13 07:00", "2026-10-19 09:00"},
		{"weekly sunday 18:00", "2026-10-13 07:00", "2026-10-18 18:00"},
		{"Weekly Fri 17:30", "2026-10-16 17:29", "2026-10-16 17:30"},
		{"*/15 * * * *", "2026-10-13 09:07", "2026-10-13 09:15"},
		{"0 9 * * 1-5", "2026-10-17 10:00", "2026-10-19 09:00"},
		{"30 2 1 * *", "2026-10-13 00:00", "2026-11-01 02:30"},
		{"0 0 29 2 *", "2026-03-01 00:00", "2028-02-29 00:00"},
		{"0 12 13 * 5", "2026-10-13 13:00", "2026-10-16 12:00"},
		{"0,30 8-9 * * *", "2026-10-13 08:45", "2026-10-13 09:00"},
		{"0 0 * * 7", "2026-10-13 00:00", "2026-10-18 00:00"},
		{"5 * * jan,oct *", "2026-10-31 23:30", "2027-01-01 00:05"},
	}
	for _, tc := range cases {
		t.Run(tc.sched+" after "+tc.after, func(t *testing.T) {
			s, err := ParseSchedule(tc.sched)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Next(at(tc.after), utc)
			if !got.Equal(at(tc.want)) {
				t.Errorf("got %s, want %s", got.Format("2006-01-02 15:04 Mon"), tc.want)
			}
		})
	}
}

func TestNextRunAcrossDSTChanges(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	daily, err := ParseSchedule("daily 02:30")
	if err != nil {
		t.Fatal(err)
	}
	beforeSpring := time.Date(2027, 3, 13, 12, 0, 0, 0, ny)
	got := daily.Next(beforeSpring, ny)
	if want := time.Date(2027, 3, 14, 3, 30, 0, 0, ny); !got.Equal(want) {
		t.Errorf("spring gap: got %s, want %s", got, want)
	}
	if next := daily.Next(got, ny); !next.Equal(time.Date(2027, 3, 15, 2, 30, 0, 0, ny)) {
		t.Errorf("after the gap: got %s", next)
	}
	fall, _ := ParseSchedule("daily 01:30")
	beforeFall := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	first := fall.Next(beforeFall, ny)
	if first.Hour() != 1 || first.Minute() != 30 || first.Day() != 1 {
		t.Fatalf("fall back: got %s", first)
	}
	if again := fall.Next(first, ny); again.Day() != 2 {
		t.Errorf("fall back ran twice: %s then %s", first, again)
	}
	hourly, _ := ParseSchedule("hourly")
	if got := hourly.Next(time.Date(2027, 3, 14, 1, 30, 0, 0, ny), ny); got.Sub(time.Date(2027, 3, 14, 1, 30, 0, 0, ny)) != 30*time.Minute {
		t.Errorf("hourly over the gap: %s", got)
	}
}

func TestScheduleDecision(t *testing.T) {
	daily, _ := ParseSchedule("daily 09:00")
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	grace := 15 * time.Minute
	cases := []struct {
		name    string
		last    time.Time
		now     time.Time
		action  string
		due     time.Time
		created time.Time
	}{
		{"next run in the future", day(13, 9, 0), day(13, 12, 0), ScheduleWait, day(14, 9, 0), time.Time{}},
		{"due now", day(12, 9, 0), day(13, 9, 0), ScheduleRun, day(13, 9, 0), time.Time{}},
		{"late inside grace", day(12, 9, 0), day(13, 9, 10), ScheduleRun, day(13, 9, 0), time.Time{}},
		{"late past grace is missed", day(12, 9, 0), day(13, 9, 16), ScheduleMissed, day(13, 9, 0), time.Time{}},
		{"many missed days record the newest", day(5, 9, 0), day(13, 12, 0), ScheduleMissed, day(13, 9, 0), time.Time{}},
		{"many missed days, newest inside grace runs", day(5, 9, 0), day(13, 9, 5), ScheduleRun, day(13, 9, 0), time.Time{}},
		{"never ran waits from creation", time.Time{}, day(13, 12, 0), ScheduleWait, day(14, 9, 0), day(13, 11, 0)},
		{"never ran but due since creation", time.Time{}, day(13, 9, 5), ScheduleRun, day(13, 9, 0), day(13, 8, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			since := tc.last
			if since.IsZero() {
				since = tc.created
			}
			action, due := ScheduleDecision(daily, time.UTC, since, tc.now, grace)
			if action != tc.action || !due.Equal(tc.due) {
				t.Errorf("got %s at %s, want %s at %s", action, due, tc.action, tc.due)
			}
		})
	}
}

func TestValidateAutomationChecksTheSchedule(t *testing.T) {
	a := validAutomation()
	a.Schedule = "daily 25:00"
	if err := ValidateAutomation(a); err == nil || !strings.Contains(err.Error(), "schedule") {
		t.Errorf("got %v, want a schedule error", err)
	}
	a.Schedule = "weekdays 08:30"
	if err := ValidateAutomation(a); err != nil {
		t.Errorf("got %v", err)
	}
}

func TestAutomationHashChangesWithTheDefinition(t *testing.T) {
	a := validAutomation()
	b := a
	b.Precheck = "rm -rf /"
	c := a
	c.Enabled = false
	if AutomationHash(a) == AutomationHash(b) {
		t.Error("a new precheck kept the hash")
	}
	if AutomationHash(a) != AutomationHash(c) {
		t.Error("enabled is not part of what the user trusts")
	}
}
