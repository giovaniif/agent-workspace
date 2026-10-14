package codex

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/giovaniif/crew/internal/domain"
)

var ErrUnmapped = errors.New("codex event is not mapped")

type Observation struct {
	Event          domain.HarnessEvent
	SessionID      string
	Cwd            string
	Model          string
	TranscriptPath string
}

var notifyKinds = map[string]domain.HarnessEventKind{
	"agent-turn-complete": domain.EventStop,
}

type hookPayload struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	Model          string `json:"model"`
	TranscriptPath string `json:"transcript_path"`
}

func ParseHook(name string, stdin []byte) (Observation, error) {
	kind, ok := domain.HookEvent(domain.HarnessCodex, name)
	if !ok {
		return Observation{}, fmt.Errorf("%w: hook %q", ErrUnmapped, name)
	}
	var p hookPayload
	_ = json.Unmarshal(stdin, &p)
	return Observation{
		Event:          domain.HarnessEvent{Kind: kind},
		SessionID:      p.SessionID,
		Cwd:            p.Cwd,
		Model:          p.Model,
		TranscriptPath: p.TranscriptPath,
	}, nil
}

type notifyPayload struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread-id"`
	Cwd      string `json:"cwd"`
}

func ParseNotify(arg string) (Observation, error) {
	var p notifyPayload
	if err := json.Unmarshal([]byte(arg), &p); err != nil {
		return Observation{}, fmt.Errorf("codex notify payload: %w", err)
	}
	kind, ok := notifyKinds[p.Type]
	if !ok {
		return Observation{}, fmt.Errorf("%w: notify %q", ErrUnmapped, p.Type)
	}
	return Observation{
		Event:     domain.HarnessEvent{Kind: kind},
		SessionID: p.ThreadID,
		Cwd:       p.Cwd,
	}, nil
}
