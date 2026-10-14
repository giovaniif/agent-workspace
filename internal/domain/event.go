package domain

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const SessionEventsKept = 20

const (
	MaxDetail = 80
	MaxText   = 4000
)

type SessionEvent struct {
	SessionID string
	At        time.Time
	Kind      HarnessEventKind
	Tool      string
	Detail    string
	Text      string
}

var toolInputFields = []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"}

func SessionEventFromHook(kind HarnessEventKind, at time.Time, payload []byte) SessionEvent {
	ev := SessionEvent{Kind: kind, At: at}
	var p struct {
		ToolName             string         `json:"tool_name"`
		ToolInput            map[string]any `json:"tool_input"`
		Message              string         `json:"message"`
		LastAssistantMessage string         `json:"last_assistant_message"`
		Prompt               string         `json:"prompt"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return ev
	}
	ev.Tool = p.ToolName
	input := primaryInput(p.ToolInput)
	ev.Detail = cutRunes(firstLine(input), MaxDetail)
	switch kind {
	case EventPermissionRequest:
		ev.Text = p.Message
		if ev.Text == "" && p.ToolName != "" {
			ev.Text = p.ToolName + ": " + input
		}
	case EventWaitingForInput:
		ev.Text = p.Message
	case EventStop:
		ev.Text = p.LastAssistantMessage
	case EventUserPromptSubmit:
		ev.Text = p.Prompt
	}
	ev.Text = cutBytes(ev.Text, MaxText)
	return ev
}

func primaryInput(input map[string]any) string {
	for _, f := range toolInputFields {
		if s, ok := input[f].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
