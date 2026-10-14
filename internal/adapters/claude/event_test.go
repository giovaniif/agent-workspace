package claude_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/domain"
)

func TestEveryHookFixtureMovesTheSessionToItsState(t *testing.T) {
	cases := []struct {
		fixture string
		from    domain.AgentState
		want    domain.AgentState
	}{
		{"SessionStart", domain.StateDone, domain.StateIdle},
		{"UserPromptSubmit", domain.StateIdle, domain.StateRunning},
		{"PreToolUse", domain.StateWaiting, domain.StateRunning},
		{"PostToolUse", domain.StatePermission, domain.StateRunning},
		{"PermissionRequest", domain.StateRunning, domain.StatePermission},
		{"Notification.permission_prompt", domain.StateRunning, domain.StatePermission},
		{"Notification.idle_prompt", domain.StateRunning, domain.StateWaiting},
		{"Stop", domain.StateRunning, domain.StateDone},
		{"SubagentStart", domain.StateWaiting, domain.StateRunning},
		{"SubagentStop", domain.StateWaiting, domain.StateRunning},
		{"SessionEnd", domain.StateRunning, domain.StateIdle},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			payload, err := os.ReadFile(filepath.Join("testdata", "hooks", c.fixture+".json"))
			if err != nil {
				t.Fatal(err)
			}
			name := c.fixture
			if i := len("Notification"); len(name) > i && name[:i] == "Notification" {
				name = "Notification"
			}
			kind, ok := claude.Event(name, payload)
			if !ok {
				t.Fatalf("Event(%q) not mapped", name)
			}
			got, _ := domain.Session{State: c.from}.Apply(domain.HarnessEvent{Kind: kind})
			if got.State != c.want {
				t.Fatalf("%s from %s = %s, want %s", c.fixture, c.from, got.State, c.want)
			}
		})
	}
}

func TestEventIgnoresUnknownHooksAndAuthNotifications(t *testing.T) {
	if _, ok := claude.Event("PreCompact", []byte(`{}`)); ok {
		t.Error("PreCompact mapped")
	}
	if _, ok := claude.Event("Notification", []byte(`{"notification_type":"auth_success"}`)); ok {
		t.Error("auth_success mapped")
	}
}

func TestEventWithoutAPayloadStillMaps(t *testing.T) {
	if kind, ok := claude.Event("Notification", nil); !ok || kind != domain.EventWaitingForInput {
		t.Fatalf("got %q, %v", kind, ok)
	}
}
