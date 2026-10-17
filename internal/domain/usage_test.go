package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestUsageReportStampsWhenTheLimitsWereReported(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	s := Session{LimitsAt: at.Add(-time.Hour), Limits: []RateLimit{{Window: "five_hour", UsedPercent: 10}}}
	got := s.Report(StatusReport{ContextLeft: 50, HasContext: true, At: at})
	if !got.LimitsAt.Equal(s.LimitsAt) {
		t.Fatalf("a report without limits moved LimitsAt to %s", got.LimitsAt)
	}
	got = s.Report(StatusReport{Limits: []RateLimit{{Window: "five_hour", UsedPercent: 20}}, At: at})
	if !got.LimitsAt.Equal(at) {
		t.Fatalf("LimitsAt = %s, want %s", got.LimitsAt, at)
	}
}

func TestReportSetsModelEffortAndUsage(t *testing.T) {
	limits := []RateLimit{{Window: "five_hour", UsedPercent: 57}, {Window: "seven_day", UsedPercent: 71}, {Window: "seven_day_opus", UsedPercent: 12}}
	s := Session{Model: "old", Effort: "low", State: StateRunning}
	got := s.Report(StatusReport{Model: "Opus", Effort: "high", ContextLeft: 94, HasContext: true, Limits: limits})
	if got.Model != "Opus" || got.Effort != "high" || got.State != StateRunning {
		t.Fatalf("got %+v", got)
	}
	if want := (Usage{ContextLeftPercent: 94, HasContext: true, LimitUsedPercent: 71}); got.Usage != want {
		t.Fatalf("usage = %+v, want %+v", got.Usage, want)
	}
	if !reflect.DeepEqual(got.Limits, limits) {
		t.Fatalf("limits = %+v", got.Limits)
	}
}

func TestReportKeepsTheHighestTokenTotal(t *testing.T) {
	s := Session{}.Report(StatusReport{Tokens: 900})
	if s.Usage.Tokens != 900 {
		t.Fatalf("tokens %d", s.Usage.Tokens)
	}
	if s = s.Report(StatusReport{}); s.Usage.Tokens != 900 {
		t.Fatalf("a report with no tokens changed them to %d", s.Usage.Tokens)
	}
	if s = s.Report(StatusReport{Tokens: 1200}); s.Usage.Tokens != 1200 {
		t.Fatalf("tokens %d", s.Usage.Tokens)
	}
}

func TestReportKeepsWhatTheStatusLineDoesNotKnowYet(t *testing.T) {
	s := Session{Model: "Opus", Effort: "high", Usage: Usage{ContextLeftPercent: 40, LimitUsedPercent: 30}, Limits: []RateLimit{{Window: "five_hour", UsedPercent: 30}}}
	got := s.Report(StatusReport{})
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("got %+v, want %+v", got, s)
	}
}

func TestReportWithZeroContextLeftIsKnown(t *testing.T) {
	got := Session{Usage: Usage{ContextLeftPercent: 40}}.Report(StatusReport{HasContext: true})
	if got.Usage.ContextLeftPercent != 0 || !got.Usage.HasContext {
		t.Fatalf("usage = %+v; want 0%% left, known", got.Usage)
	}
}
