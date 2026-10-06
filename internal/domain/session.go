package domain

import "time"

type AgentState string

const (
	StateIdle       AgentState = "idle"
	StateRunning    AgentState = "running"
	StateWaiting    AgentState = "waiting"
	StatePermission AgentState = "permission"
	StateDone       AgentState = "done"
)

type HarnessEventKind string

const (
	EventSessionStart      HarnessEventKind = "session_start"
	EventUserPromptSubmit  HarnessEventKind = "user_prompt_submit"
	EventPreToolUse        HarnessEventKind = "pre_tool_use"
	EventPostToolUse       HarnessEventKind = "post_tool_use"
	EventPermissionRequest HarnessEventKind = "permission_request"
	EventWaitingForInput   HarnessEventKind = "waiting_for_input"
	EventStop              HarnessEventKind = "stop"
	EventSessionEnd        HarnessEventKind = "session_end"
	EventSubagentStart     HarnessEventKind = "subagent_start"
	EventSubagentStop      HarnessEventKind = "subagent_stop"
)

type HarnessEvent struct {
	Kind HarnessEventKind
}

type EffectKind string

const (
	EffectNotify     EffectKind = "notify"
	EffectMarkUnread EffectKind = "mark_unread"
)

type Effect struct {
	Kind  EffectKind
	State AgentState
}

type Session struct {
	ID             string
	TaskID         string
	Harness        Harness
	Pane           string
	Model          string
	Effort         string
	StartedAt      time.Time `json:",omitzero"`
	StartModel     string    `json:",omitempty"`
	StartEffort    string    `json:",omitempty"`
	StartWorkspace string    `json:",omitempty"`
	State          AgentState
	Ended          bool
	Unread         bool
	Focused        bool
	Muted          bool
	WorktreeIDs    []string
	ResumeID       string
	Transcript     string
	Dir            string
	Usage          Usage
	Limits         []RateLimit
	LimitsAt       time.Time
	Switches       []Switch
	SwitchWarning  bool
}

func (s Session) Apply(ev HarnessEvent) (Session, []Effect) {
	switch ev.Kind {
	case EventSessionStart, EventSessionEnd:
		s.State = StateIdle
		return s, nil
	case EventUserPromptSubmit:
		s.State = StateRunning
		s.Unread = false
		return s, nil
	case EventPreToolUse, EventPostToolUse, EventSubagentStart, EventSubagentStop:
		if s.inTurn() {
			s.State = StateRunning
		}
		return s, nil
	case EventPermissionRequest:
		return s.enterAttention(StatePermission)
	case EventWaitingForInput:
		return s.enterAttention(StateWaiting)
	case EventStop:
		if s.State == StateDone {
			return s, nil
		}
		s.State = StateDone
		effects := []Effect{{Kind: EffectNotify, State: StateDone}}
		if !s.Focused {
			s.Unread = true
			effects = append(effects, Effect{Kind: EffectMarkUnread})
		}
		return s, effects
	}
	return s, nil
}

func (s Session) enterAttention(next AgentState) (Session, []Effect) {
	if !s.inTurn() || s.State == next {
		return s, nil
	}
	s.State = next
	return s, []Effect{{Kind: EffectNotify, State: next}}
}

func (s Session) inTurn() bool {
	return s.State != StateIdle && s.State != StateDone
}

func (s Session) Focus() Session {
	s.Focused = true
	s.Unread = false
	return s
}

func (s Session) Blur() Session {
	s.Focused = false
	return s
}

func (s Session) AttachWorktree(id string) Session {
	for _, w := range s.WorktreeIDs {
		if w == id {
			return s
		}
	}
	s.WorktreeIDs = append(append([]string(nil), s.WorktreeIDs...), id)
	return s
}

func (s Session) DetachWorktree(id string) Session {
	kept := make([]string, 0, len(s.WorktreeIDs))
	for _, w := range s.WorktreeIDs {
		if w != id {
			kept = append(kept, w)
		}
	}
	s.WorktreeIDs = kept
	if s.Dir == id {
		s.Dir = ""
	}
	return s
}

func LastCreatedSession(all []Session) (Session, bool) {
	var best Session
	found := false
	for _, s := range all {
		if s.StartedAt.IsZero() {
			continue
		}
		if !found || s.StartedAt.After(best.StartedAt) {
			best, found = s, true
		}
	}
	return best, found
}
