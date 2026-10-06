package domain

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

var allStates = []AgentState{
	StateIdle, StateRunning, StateWaiting, StatePermission, StateDone,
}

var allEvents = []HarnessEventKind{
	EventSessionStart, EventUserPromptSubmit, EventPreToolUse,
	EventPostToolUse, EventPermissionRequest, EventWaitingForInput,
	EventStop, EventSessionEnd, EventSubagentStart, EventSubagentStop,
}

type transition struct {
	next    AgentState
	effects []Effect
}

func notify(s AgentState) Effect {
	return Effect{Kind: EffectNotify, State: s}
}

var markUnread = Effect{Kind: EffectMarkUnread}

var doneEffects = []Effect{notify(StateDone), markUnread}

var expected = map[AgentState]map[HarnessEventKind]transition{
	StateIdle: {
		EventSessionStart:      {StateIdle, nil},
		EventUserPromptSubmit:  {StateRunning, nil},
		EventPreToolUse:        {StateIdle, nil},
		EventPostToolUse:       {StateIdle, nil},
		EventSubagentStart:     {StateIdle, nil},
		EventSubagentStop:      {StateIdle, nil},
		EventPermissionRequest: {StateIdle, nil},
		EventWaitingForInput:   {StateIdle, nil},
		EventStop:              {StateDone, doneEffects},
		EventSessionEnd:        {StateIdle, nil},
	},
	StateRunning: {
		EventSessionStart:      {StateIdle, nil},
		EventUserPromptSubmit:  {StateRunning, nil},
		EventPreToolUse:        {StateRunning, nil},
		EventPostToolUse:       {StateRunning, nil},
		EventSubagentStart:     {StateRunning, nil},
		EventSubagentStop:      {StateRunning, nil},
		EventPermissionRequest: {StatePermission, []Effect{notify(StatePermission)}},
		EventWaitingForInput:   {StateWaiting, []Effect{notify(StateWaiting)}},
		EventStop:              {StateDone, doneEffects},
		EventSessionEnd:        {StateIdle, nil},
	},
	StateWaiting: {
		EventSessionStart:      {StateIdle, nil},
		EventUserPromptSubmit:  {StateRunning, nil},
		EventPreToolUse:        {StateRunning, nil},
		EventPostToolUse:       {StateRunning, nil},
		EventSubagentStart:     {StateRunning, nil},
		EventSubagentStop:      {StateRunning, nil},
		EventPermissionRequest: {StatePermission, []Effect{notify(StatePermission)}},
		EventWaitingForInput:   {StateWaiting, nil},
		EventStop:              {StateDone, doneEffects},
		EventSessionEnd:        {StateIdle, nil},
	},
	StatePermission: {
		EventSessionStart:      {StateIdle, nil},
		EventUserPromptSubmit:  {StateRunning, nil},
		EventPreToolUse:        {StateRunning, nil},
		EventPostToolUse:       {StateRunning, nil},
		EventSubagentStart:     {StateRunning, nil},
		EventSubagentStop:      {StateRunning, nil},
		EventPermissionRequest: {StatePermission, nil},
		EventWaitingForInput:   {StateWaiting, []Effect{notify(StateWaiting)}},
		EventStop:              {StateDone, doneEffects},
		EventSessionEnd:        {StateIdle, nil},
	},
	StateDone: {
		EventSessionStart:      {StateIdle, nil},
		EventUserPromptSubmit:  {StateRunning, nil},
		EventPreToolUse:        {StateDone, nil},
		EventPostToolUse:       {StateDone, nil},
		EventSubagentStart:     {StateDone, nil},
		EventSubagentStop:      {StateDone, nil},
		EventPermissionRequest: {StateDone, nil},
		EventWaitingForInput:   {StateDone, nil},
		EventStop:              {StateDone, nil},
		EventSessionEnd:        {StateIdle, nil},
	},
}

func TestApplyCoversEveryStateEventPair(t *testing.T) {
	for _, state := range allStates {
		for _, ev := range allEvents {
			t.Run(fmt.Sprintf("%s/%s", state, ev), func(t *testing.T) {
				want, ok := expected[state][ev]
				if !ok {
					t.Fatalf("missing expectation for (%s, %s)", state, ev)
				}
				got, effects := Session{State: state}.Apply(HarnessEvent{Kind: ev})
				if got.State != want.next {
					t.Errorf("state = %s, want %s", got.State, want.next)
				}
				if !slices.Equal(effects, want.effects) {
					t.Errorf("effects = %v, want %v", effects, want.effects)
				}
				if got.Unread != slices.Contains(want.effects, markUnread) {
					t.Errorf("unread = %v", got.Unread)
				}
			})
		}
	}
}

func TestApplyUnknownEventIsIgnored(t *testing.T) {
	s := Session{State: StateRunning}
	got, effects := s.Apply(HarnessEvent{Kind: "bogus"})
	if got.State != s.State || effects != nil {
		t.Errorf("got %+v %v, want unchanged", got, effects)
	}
}

func TestOutOfOrderStopBeforePreToolUseStaysDone(t *testing.T) {
	s := Session{State: StateIdle}
	for _, ev := range []HarnessEventKind{
		EventUserPromptSubmit, EventStop, EventPreToolUse, EventPostToolUse,
	} {
		s, _ = s.Apply(HarnessEvent{Kind: ev})
	}
	if s.State != StateDone || !s.Unread {
		t.Errorf("got %s unread=%v, want done unread", s.State, s.Unread)
	}
}

func TestDoneWhileFocusedDoesNotMarkUnread(t *testing.T) {
	s := Session{State: StateRunning}.Focus()
	got, effects := s.Apply(HarnessEvent{Kind: EventStop})
	if got.Unread {
		t.Error("focused session marked unread")
	}
	if !slices.Equal(effects, []Effect{notify(StateDone)}) {
		t.Errorf("effects = %v", effects)
	}
}

func TestFocusClearsUnreadAndBlurKeepsItCleared(t *testing.T) {
	s, _ := Session{State: StateRunning}.Apply(HarnessEvent{Kind: EventStop})
	if !s.Unread {
		t.Fatal("precondition: unread")
	}
	s = s.Focus()
	if s.Unread || !s.Focused {
		t.Errorf("after focus: unread=%v focused=%v", s.Unread, s.Focused)
	}
	s = s.Blur()
	if s.Unread || s.Focused {
		t.Errorf("after blur: unread=%v focused=%v", s.Unread, s.Focused)
	}
	s, _ = s.Apply(HarnessEvent{Kind: EventUserPromptSubmit})
	s, _ = s.Apply(HarnessEvent{Kind: EventStop})
	if !s.Unread {
		t.Error("blurred session not marked unread")
	}
}

func TestUserPromptClearsUnread(t *testing.T) {
	s := Session{State: StateDone, Unread: true}
	got, _ := s.Apply(HarnessEvent{Kind: EventUserPromptSubmit})
	if got.Unread {
		t.Error("prompt left session unread")
	}
}

func TestApplyKeepsOtherFields(t *testing.T) {
	s := Session{ID: "s1", Harness: HarnessCodex, Model: "m", WorktreeIDs: []string{"w"}}
	got, _ := s.Apply(HarnessEvent{Kind: EventUserPromptSubmit})
	if got.ID != "s1" || got.Harness != HarnessCodex || got.Model != "m" || len(got.WorktreeIDs) != 1 {
		t.Errorf("fields lost: %+v", got)
	}
}

func TestLastCreatedSessionIgnoresOnesWithoutAStartTime(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	all := []Session{
		{ID: "unstamped", Harness: HarnessClaude},
		{ID: "older", StartedAt: t0, StartModel: "sonnet"},
		{ID: "newer", StartedAt: t0.Add(time.Hour), StartModel: "gpt-5"},
	}
	got, ok := LastCreatedSession(all)
	if !ok || got.ID != "newer" {
		t.Fatalf("LastCreatedSession = %+v, %v", got, ok)
	}
	if _, ok := LastCreatedSession([]Session{{ID: "unstamped"}}); ok {
		t.Fatal("a session without StartedAt counted as created")
	}
}
