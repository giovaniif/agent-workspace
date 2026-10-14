package domain

type SwitchForm int

const (
	SwitchNone SwitchForm = iota
	SwitchSlash
	SwitchPicker
	SwitchOmpSwitch
)

type HarnessSpec struct {
	Harness Harness
	Name    string
	Tag     string
	Hooks   map[string]HarnessEventKind
	Switch  SwitchForm
	Models  []string
	Efforts []string

	DefaultModel  string
	DefaultEffort string
}

var harnessTable = []HarnessSpec{
	{
		Harness: HarnessClaude,
		Name:    "Claude Code",
		Tag:     "CC",
		Switch:  SwitchSlash,
		Models:  []string{"opus", "sonnet", "haiku"},
		Efforts: []string{"low", "medium", "high", "xhigh", "max"},

		DefaultModel:  "opus",
		DefaultEffort: "high",
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

		DefaultModel:  "gpt-6.1-sol",
		DefaultEffort: "medium",
		Hooks: map[string]HarnessEventKind{
			"SessionStart":      EventSessionStart,
			"UserPromptSubmit":  EventUserPromptSubmit,
			"PreToolUse":        EventPreToolUse,
			"PostToolUse":       EventPostToolUse,
			"Stop":              EventStop,
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
		Hooks: map[string]HarnessEventKind{
			"session_start":           EventSessionStart,
			"agent_start":             EventUserPromptSubmit,
			"tool_call":               EventPreToolUse,
			"tool_result":             EventPostToolUse,
			"tool_approval_requested": EventPermissionRequest,
			"tool_approval_resolved":  EventWaitingForInput,
			"agent_end":               EventStop,
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
