package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestNewMateBoardSortsByNameAndSetsEachGuard(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	busy := MateUsage{}.AddTurn(now.Add(-time.Minute))
	states := []MateStatus{
		{Name: "webby", State: MateConnected},
		{Name: "helper", State: MatePaused, Guard: GuardStopped},
	}
	limits := MateLimits{TurnsPerHour: 1}
	got := NewMateBoard(states, limits, map[string]MateUsage{"webby": busy}, now)
	want := MateBoard{Mates: []MateStatus{
		{Name: "helper", State: MatePaused},
		{Name: "webby", State: MateConnected, Guard: GuardRate},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestNewMateBoardOfAGlobalStopStopsEveryMate(t *testing.T) {
	got := NewMateBoard([]MateStatus{{Name: "a"}, {Name: "b"}}, MateLimits{Stop: true}, nil, time.Now())
	if !got.Stopped || got.Mates[0].Guard != GuardStopped || got.Mates[1].Guard != GuardStopped {
		t.Fatalf("board %+v", got)
	}
}

func TestNewMateBoardOfNoMatesIsAnEmptyList(t *testing.T) {
	if got := NewMateBoard(nil, MateLimits{}, nil, time.Now()); got.Mates == nil || len(got.Mates) != 0 {
		t.Fatalf("board %+v", got)
	}
}

func TestMateThreadURL(t *testing.T) {
	tests := []struct {
		team, channel, ts, want string
	}{
		{"T1", "C1", "1700.0001", "https://app.slack.com/client/T1/C1/thread/C1-1700.0001"},
		{"", "C1", "1700.0001", ""},
		{"T1", "", "1700.0001", ""},
		{"T1", "C1", "", ""},
	}
	for _, tt := range tests {
		if got := MateThreadURL(tt.team, tt.channel, tt.ts); got != tt.want {
			t.Errorf("MateThreadURL(%q, %q, %q) = %q, want %q", tt.team, tt.channel, tt.ts, got, tt.want)
		}
	}
}

func TestMateLinksSortsByMateThenThreadAndKeepsTheSession(t *testing.T) {
	threads := []MateThread{
		{Team: "T1", Channel: "C1", ThreadTS: "2", Mate: "b", SessionID: "s3"},
		{Team: "T1", Channel: "C1", ThreadTS: "2", Mate: "a", SessionID: "s2"},
		{Team: "T1", Channel: "C1", ThreadTS: "1", Mate: "a", SessionID: "s1"},
	}
	got := MateLinks(threads)
	var order []string
	for _, l := range got {
		order = append(order, l.SessionID)
	}
	if !reflect.DeepEqual(order, []string{"s1", "s2", "s3"}) {
		t.Fatalf("order %v", order)
	}
	if got[0].URL != "https://app.slack.com/client/T1/C1/thread/C1-1" || got[0].Mate != "a" || got[0].Channel != "C1" || got[0].ThreadTS != "1" || got[0].Team != "T1" {
		t.Fatalf("link %+v", got[0])
	}
	if l := MateLinks(nil); l == nil {
		t.Fatal("nil links")
	}
}

func TestMateWorking(t *testing.T) {
	for state, want := range map[AgentState]bool{
		StateRunning: true, StatePermission: true, StateWaiting: true,
		StateIdle: false, StateDone: false,
	} {
		if got := MateWorking(state); got != want {
			t.Errorf("MateWorking(%q) = %v, want %v", state, got, want)
		}
	}
}
