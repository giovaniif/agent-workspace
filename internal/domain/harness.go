package domain

type SwitchForm int

const (
	SwitchNone SwitchForm = iota
	SwitchSlash
	// why: Codex's /model takes no argument; it opens a picker that is walked by keys.
	SwitchPicker
	// why: omp's /model drops its argument and opens a picker, while /switch
	// takes a model id or model:level.
	SwitchOmpSwitch
)

type HarnessSpec struct {
	Harness Harness
	Name    string
	Tag     string
	Hooks   map[string]HarnessEventKind
	Switch  SwitchForm
	// why: nil means the harness takes any model id, typed rather than picked.
	Models []string
	// why: each harness confirms a different set, so one shared list would send omp a level it rejects.
	Efforts []string
}

var harnessTable = []HarnessSpec{
	{
		Harness: HarnessClaude,
		Name:    "Claude Code",
		Tag:     "CC",
		Switch:  SwitchSlash,
		Models:  []string{"opus", "sonnet", "haiku"},
		Efforts: []string{"low", "medium", "high", "xhigh", "max"},
		Hooks: map[string]HarnessEventKind{
			"SessionStart":      EventSessionStart,
			"UserPromptSubmit":  EventUserPromptSubmit,
			"PreToolUse":        EventPreToolUse,
			"PostToolUse":       EventPostToolUse,
			"PermissionRequest": EventPermissionRequest,
			"Notification":      EventWaitingForInput,
			"Stop":              EventStop,
			"SubagentStart":     EventSubagentStart,
			"SubagentStop":      EventSubagentStop,
			"SessionEnd":        EventSessionEnd,
		},
	},
	{
		Harness: HarnessCodex,
		Name:    "Codex",
		Tag:     "CX",
		Switch:  SwitchPicker,
		Models:  []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.5"},
		Efforts: []string{"low", "medium", "high", "xhigh", "max"},
		Hooks: map[string]HarnessEventKind{
			"SessionStart":     EventSessionStart,
			"UserPromptSubmit": EventUserPromptSubmit,
			"PreToolUse":       EventPreToolUse,
			"PostToolUse":      EventPostToolUse,
			"Stop":             EventStop,
			// why: Codex fires Interrupt instead of Stop when the user aborts a turn.
			"Interrupt":         EventStop,
			"PermissionRequest": EventPermissionRequest,
			"SessionEnd":        EventSessionEnd,
		},
	},
	{
		Harness: HarnessOmp,
		Name:    "Oh My Pi",
		Tag:     "OM",
		Switch:  SwitchOmpSwitch,
		Efforts: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
		// why: agent_end carries willContinue and turn_end is one model round, so
		// neither means done; session_switch would idle the pane on a fork. An
		// approval is not an event: the tool_call after it sets running.
		Hooks: map[string]HarnessEventKind{
			"session_start":           EventSessionStart,
			"agent_start":             EventUserPromptSubmit,
			"tool_call":               EventPreToolUse,
			"tool_result":             EventPostToolUse,
			"tool_approval_requested": EventPermissionRequest,
			"tool_approval_resolved":  EventWaitingForInput,
			"session_stop":            EventStop,
			"session_shutdown":        EventSessionEnd,
		},
	},
}

func Harnesses() []Harness {
	out := make([]Harness, len(harnessTable))
	for i, spec := range harnessTable {
		out[i] = spec.Harness
	}
	return out
}

func Spec(h Harness) HarnessSpec {
	for _, spec := range harnessTable {
		if spec.Harness == h {
			return spec
		}
	}
	return HarnessSpec{Harness: h}
}
