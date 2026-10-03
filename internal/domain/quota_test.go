package domain

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func limited(h Harness, at time.Time, limits ...RateLimit) Session {
	return Session{Harness: h, Limits: limits, LimitsAt: at}
}

func TestUsageQuotasAreLeftPercentPerHarnessWindowShortestFirst(t *testing.T) {
	got := Quotas([]Session{
		limited(HarnessCodex, t0, RateLimit{Window: "seven_day", UsedPercent: 12, ResetsAt: 900}, RateLimit{Window: "five_hour", UsedPercent: 41, ResetsAt: 800}),
		limited(HarnessClaude, t0, RateLimit{Window: "seven_day_opus", UsedPercent: 88}, RateLimit{Window: "five_hour", UsedPercent: 57, ResetsAt: 700}, RateLimit{Window: "seven_day", UsedPercent: 71}),
		{Harness: HarnessClaude},
	})
	want := []Quota{
		{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 43, ResetsAt: 700, ReportedAt: t0},
		{Harness: HarnessClaude, Window: "seven_day", LeftPercent: 29, ReportedAt: t0},
		{Harness: HarnessClaude, Window: "seven_day_opus", LeftPercent: 12, ReportedAt: t0},
		{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 59, ResetsAt: 800, ReportedAt: t0},
		{Harness: HarnessCodex, Window: "seven_day", LeftPercent: 88, ResetsAt: 900, ReportedAt: t0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestUsageQuotasTakeEachWindowFromTheNewestReport(t *testing.T) {
	older := limited(HarnessClaude, t0, RateLimit{Window: "five_hour", UsedPercent: 10}, RateLimit{Window: "seven_day", UsedPercent: 20})
	newer := limited(HarnessClaude, t0.Add(time.Minute), RateLimit{Window: "five_hour", UsedPercent: 30})
	for _, sessions := range [][]Session{{older, newer}, {newer, older}} {
		got := Quotas(sessions)
		if len(got) != 2 || got[0].LeftPercent != 70 || !got[0].ReportedAt.Equal(newer.LimitsAt) || got[1].LeftPercent != 80 {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestUsageQuotaLeftIsClampedToZeroAndOneHundred(t *testing.T) {
	got := Quotas([]Session{limited(HarnessClaude, t0, RateLimit{Window: "five_hour", UsedPercent: 130}, RateLimit{Window: "seven_day", UsedPercent: -4})})
	if got[0].LeftPercent != 0 || got[1].LeftPercent != 100 {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageNoLimitsMeansNoQuotas(t *testing.T) {
	if got := Quotas([]Session{{Harness: HarnessClaude}, {Harness: HarnessCodex}}); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	if got := Quotas(nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageQuotaIsLowBelowTwentyPercentLeft(t *testing.T) {
	for left, want := range map[int]bool{0: true, 19: true, 20: false, 21: false, 100: false} {
		if got := (Quota{LeftPercent: left}).Low(); got != want {
			t.Errorf("left %d: Low = %v, want %v", left, got, want)
		}
	}
}

func TestUsageQuotaIsStaleAfterFifteenMinutes(t *testing.T) {
	q := Quota{ReportedAt: t0}
	for age, want := range map[time.Duration]bool{0: false, 15 * time.Minute: false, 15*time.Minute + time.Second: true, 2 * time.Hour: true} {
		if got := q.Stale(t0.Add(age)); got != want {
			t.Errorf("age %s: Stale = %v, want %v", age, got, want)
		}
	}
	if got := q.Age(t0.Add(-time.Minute)); got != 0 {
		t.Errorf("age before the report = %s", got)
	}
}

func TestUsageWindowLabels(t *testing.T) {
	for window, want := range map[string]string{
		"five_hour":      "5h",
		"seven_day":      "7d",
		"seven_day_opus": "7d opus",
		"90m":            "1h30m",
		"300m":           "5h",
		"45m":            "45m",
		"weird":          "weird",
	} {
		if got := WindowLabel(window); got != want {
			t.Errorf("WindowLabel(%q) = %q, want %q", window, got, want)
		}
	}
}

func TestUsageWindowNameForMinutes(t *testing.T) {
	for minutes, want := range map[int]string{300: "five_hour", 10080: "seven_day", 60: "60m", 0: "0m"} {
		if got := WindowNameForMinutes(minutes); got != want {
			t.Errorf("WindowNameForMinutes(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestUsageAdviseWarnsBelowTwentyFivePercentOnTheShortestWindow(t *testing.T) {
	quotas := []Quota{
		{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 24},
		{Harness: HarnessClaude, Window: "seven_day", LeftPercent: 90},
		{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 80},
		{Harness: HarnessCodex, Window: "seven_day", LeftPercent: 10},
	}
	got, ok := Advise(quotas, HarnessClaude)
	if !ok || got.Low.LeftPercent != 24 || got.Other != HarnessCodex || got.OtherShortest == nil || got.OtherShortest.LeftPercent != 80 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestUsageAdviseIgnoresLongerWindowsAndTheThresholdItself(t *testing.T) {
	quotas := []Quota{
		{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 25},
		{Harness: HarnessClaude, Window: "seven_day", LeftPercent: 3},
	}
	if got, ok := Advise(quotas, HarnessClaude); ok {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageAdviseWithoutTheOtherHarnessFigureCannotOfferASwitch(t *testing.T) {
	got, ok := Advise([]Quota{{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 5}}, HarnessCodex)
	if !ok || got.Other != HarnessClaude || got.OtherShortest != nil {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestUsageAdvisePicksTheReportedAlternativeWithTheMostLeft(t *testing.T) {
	cases := []struct {
		name   string
		chosen Harness
		quotas []Quota
		other  Harness
		left   int
	}{
		{"omp has more left", HarnessClaude, []Quota{
			{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 5},
			{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 30},
			{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 60},
		}, HarnessOmp, 60},
		{"a tie goes to catalog order", HarnessClaude, []Quota{
			{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 5},
			{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 50},
			{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 50},
		}, HarnessCodex, 50},
		{"omp chosen moves to the roomiest other", HarnessOmp, []Quota{
			{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 5},
			{Harness: HarnessClaude, Window: "five_hour", LeftPercent: 40},
			{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 70},
		}, HarnessCodex, 70},
		{"a harness with no figure loses to one with a figure", HarnessCodex, []Quota{
			{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 5},
			{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 1},
		}, HarnessOmp, 1},
	}
	for _, c := range cases {
		got, ok := Advise(c.quotas, c.chosen)
		if !ok || got.Other != c.other || got.OtherShortest == nil || got.OtherShortest.LeftPercent != c.left {
			t.Errorf("%s: got %+v ok=%v", c.name, got, ok)
		}
	}
}

func TestUsageAdviseWithNoReportedAlternativeNamesTheFirstOtherInTheCatalog(t *testing.T) {
	got, ok := Advise([]Quota{{Harness: HarnessOmp, Window: "five_hour", LeftPercent: 5}}, HarnessOmp)
	if !ok || got.Other != HarnessClaude || got.OtherShortest != nil {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestUsageQuotasFollowTheCatalogOrder(t *testing.T) {
	got := Quotas([]Session{
		limited("other", t0, RateLimit{Window: "five_hour", UsedPercent: 1}),
		limited(HarnessOmp, t0, RateLimit{Window: "five_hour", UsedPercent: 1}),
		limited(HarnessCodex, t0, RateLimit{Window: "five_hour", UsedPercent: 1}),
	})
	var order []Harness
	for _, q := range got {
		order = append(order, q.Harness)
	}
	if want := []Harness{HarnessCodex, HarnessOmp, "other"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order %q, want %q", order, want)
	}
}

func TestUsageAdviseWithNoDataForTheChosenHarnessStaysQuiet(t *testing.T) {
	if got, ok := Advise([]Quota{{Harness: HarnessCodex, Window: "five_hour", LeftPercent: 1}}, HarnessClaude); ok {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageCurrentDropsWindowsWhoseResetHasPassed(t *testing.T) {
	quotas := []Quota{
		{Window: "five_hour", ResetsAt: t0.Add(-time.Second).Unix()},
		{Window: "seven_day", ResetsAt: t0.Unix()},
		{Window: "seven_day_x", ResetsAt: t0.Add(time.Second).Unix()},
		{Window: "unknown_reset"},
	}
	var got []string
	for _, q := range Current(quotas, t0) {
		got = append(got, q.Window)
	}
	if want := []string{"seven_day_x", "unknown_reset"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
