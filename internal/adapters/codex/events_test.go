package codex

import (
	"errors"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

const sessionID = "019a1c2e-7b3d-7000-8a11-2f9d5c0e1a01"

func hookJSON(event string, extra string) []byte {
	body := `{"session_id":"` + sessionID + `","transcript_path":"/home/dev/.codex/sessions/rollout-1.jsonl","cwd":"/work/api","hook_event_name":"` + event + `","model":"gpt-6.1-sol"`
	if extra != "" {
		body += "," + extra
	}
	return []byte(body + "}")
}

func TestHookEventsMoveTheSessionToTheExpectedState(t *testing.T) {
	cases := []struct {
		hook    string
		extra   string
		from    domain.AgentState
		want    domain.AgentState
		effects int
	}{
		{"SessionStart", `"source":"startup"`, domain.StateDone, domain.StateIdle, 0},
		{"UserPromptSubmit", `"prompt":"fix it"`, domain.StateIdle, domain.StateRunning, 0},
		{"PreToolUse", `"tool_name":"Bash"`, domain.StateRunning, domain.StateRunning, 0},
		{"PostToolUse", `"tool_name":"Bash"`, domain.StatePermission, domain.StateRunning, 0},
		{"PermissionRequest", `"tool_name":"Bash"`, domain.StateRunning, domain.StatePermission, 1},
		{"Stop", `"stop_hook_active":false`, domain.StateRunning, domain.StateDone, 2},
		{"Interrupt", "", domain.StateRunning, domain.StateDone, 2},
		{"Interrupt", "", domain.StatePermission, domain.StateDone, 2},
		{"SessionEnd", "", domain.StateRunning, domain.StateIdle, 0},
	}
	for _, c := range cases {
		obs, err := ParseHook(c.hook, hookJSON(c.hook, c.extra))
		if err != nil {
			t.Fatalf("%s: %v", c.hook, err)
		}
		got, effects := domain.Session{State: c.from}.Apply(obs.Event)
		if got.State != c.want || len(effects) != c.effects {
			t.Errorf("%s from %s: state %s with %d effects; want %s with %d", c.hook, c.from, got.State, len(effects), c.want, c.effects)
		}
	}
}

func TestInterruptAfterStopStaysDone(t *testing.T) {
	stop, _ := ParseHook("Stop", hookJSON("Stop", ""))
	interrupt, _ := ParseHook("Interrupt", hookJSON("Interrupt", ""))
	done, _ := domain.Session{State: domain.StateRunning}.Apply(stop.Event)
	again, effects := done.Apply(interrupt.Event)
	if again.State != domain.StateDone || len(effects) != 0 {
		t.Fatalf("state %s, effects %v", again.State, effects)
	}
}

func TestHookPayloadCarriesSessionIdentity(t *testing.T) {
	obs, err := ParseHook("PreToolUse", hookJSON("PreToolUse", `"tool_name":"Bash"`))
	if err != nil {
		t.Fatal(err)
	}
	if obs.SessionID != sessionID || obs.Cwd != "/work/api" || obs.Model != "gpt-6.1-sol" || obs.TranscriptPath != "/home/dev/.codex/sessions/rollout-1.jsonl" {
		t.Fatalf("got %+v", obs)
	}
}

func TestUnmappedHooksAreReportedNotGuessed(t *testing.T) {
	for _, name := range []string{"SubagentStop", "SubagentStart", "PreCompact", "PostCompact", "Bogus"} {
		if _, err := ParseHook(name, hookJSON(name, "")); !errors.Is(err, ErrUnmapped) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestHookWithMalformedPayloadStillMapsTheEvent(t *testing.T) {
	obs, err := ParseHook("Stop", []byte("not json"))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Event.Kind != domain.EventStop || obs.SessionID != "" {
		t.Fatalf("got %+v", obs)
	}
}

func TestNotifyPayloads(t *testing.T) {
	turn := `{"type":"agent-turn-complete","thread-id":"` + sessionID + `","turn-id":"t4","cwd":"/work/api","last-assistant-message":"done"}`
	obs, err := ParseNotify(turn)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Event.Kind != domain.EventStop || obs.SessionID != sessionID || obs.Cwd != "/work/api" {
		t.Fatalf("got %+v", obs)
	}
	if _, err := ParseNotify(`{"type":"something-new"}`); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("err %v", err)
	}
	if _, err := ParseNotify(`nope`); err == nil {
		t.Fatal("malformed notify accepted")
	}
}
