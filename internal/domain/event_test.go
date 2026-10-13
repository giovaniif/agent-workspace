package domain

import (
	"strings"
	"testing"
	"time"
)

func TestSessionEventFromHookReadsWhatTheCardShows(t *testing.T) {
	at := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		kind    HarnessEventKind
		payload string
		want    SessionEvent
	}{
		{
			name:    "tool call keeps the tool and the first line of its command",
			kind:    EventPreToolUse,
			payload: `{"tool_name":"Bash","tool_input":{"command":"go test ./...\nls"}}`,
			want:    SessionEvent{Tool: "Bash", Detail: "go test ./..."},
		},
		{
			name:    "file tools show the path",
			kind:    EventPreToolUse,
			payload: `{"tool_name":"Edit","tool_input":{"file_path":"internal/x.go","old_string":"a"}}`,
			want:    SessionEvent{Tool: "Edit", Detail: "internal/x.go"},
		},
		{
			name:    "long details are cut",
			kind:    EventPreToolUse,
			payload: `{"tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("x", 200) + `"}}`,
			want:    SessionEvent{Tool: "Bash", Detail: strings.Repeat("x", MaxDetail-1) + "…"},
		},
		{
			name:    "permission request without a message is the tool and its whole input",
			kind:    EventPermissionRequest,
			payload: `{"tool_name":"Bash","tool_input":{"command":"rm -rf build\nmake"}}`,
			want:    SessionEvent{Tool: "Bash", Detail: "rm -rf build", Text: "Bash: rm -rf build\nmake"},
		},
		{
			name:    "permission request with a message keeps it verbatim",
			kind:    EventPermissionRequest,
			payload: `{"tool_name":"Bash","message":"Allow Bash?\n  rm -rf build"}`,
			want:    SessionEvent{Tool: "Bash", Text: "Allow Bash?\n  rm -rf build"},
		},
		{
			name:    "notification message",
			kind:    EventWaitingForInput,
			payload: `{"message":"Claude is waiting for your input"}`,
			want:    SessionEvent{Text: "Claude is waiting for your input"},
		},
		{
			name:    "stop keeps the last assistant message",
			kind:    EventStop,
			payload: `{"last_assistant_message":"Done. Want me to open the PR?"}`,
			want:    SessionEvent{Text: "Done. Want me to open the PR?"},
		},
		{
			name:    "a payload that is not an object gives a bare event",
			kind:    EventStop,
			payload: `not json`,
			want:    SessionEvent{},
		},
		{
			name: "no payload gives a bare event",
			kind: EventUserPromptSubmit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionEventFromHook(tt.kind, at, []byte(tt.payload))
			tt.want.Kind, tt.want.At = tt.kind, at
			if got != tt.want {
				t.Fatalf("event = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

func TestSessionEventTextIsCappedSoOneHookCannotBloatTheStore(t *testing.T) {
	payload := `{"last_assistant_message":"` + strings.Repeat("a", 3*MaxText) + `"}`
	got := SessionEventFromHook(EventStop, time.Time{}, []byte(payload))
	if len(got.Text) != MaxText {
		t.Fatalf("text is %d bytes, want %d", len(got.Text), MaxText)
	}
}

func TestSessionEventFromHookKeepsThePromptOfAUserPromptSubmit(t *testing.T) {
	ev := SessionEventFromHook(EventUserPromptSubmit, time.Time{}, []byte(`{"prompt":"fix the flaky upload test"}`))
	if ev.Text != "fix the flaky upload test" {
		t.Fatalf("text %q", ev.Text)
	}
}
