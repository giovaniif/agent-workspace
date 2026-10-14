package codex

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

type transcriptLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type transcriptPayload struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Message   string          `json:"message"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	Status    string          `json:"status"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Action struct {
		Query string `json:"query"`
	} `json:"action"`
}

type TranscriptParser struct {
	log  domain.MessageLog
	turn string
}

func ParseTranscript(data []byte, base int64) ([]domain.Message, int64) {
	var p TranscriptParser
	return p.Parse(data, base)
}

func (p *TranscriptParser) Parse(data []byte, base int64) ([]domain.Message, int64) {
	lines, end := domain.TranscriptLines(data, base)
	for _, line := range lines {
		p.line(line)
	}
	return p.log.Drain(), end
}

func (p *TranscriptParser) line(line domain.TranscriptLine) {
	var l transcriptLine
	if json.Unmarshal(line.Data, &l) != nil {
		return
	}
	var pl transcriptPayload
	if json.Unmarshal(l.Payload, &pl) != nil {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	id := strconv.FormatInt(line.Start, 10)
	base := domain.Message{ID: id, Cursor: line.End, At: at}
	switch l.Type {
	case "event_msg":
		p.event(pl, base)
	case "response_item":
		p.item(pl, base)
	case "compacted":
		p.add(base, domain.RoleSystem, "Conversation compacted")
	}
}

func (p *TranscriptParser) event(pl transcriptPayload, base domain.Message) {
	switch pl.Type {
	case "user_message":
		p.turn = base.ID
		p.add(base, domain.RoleUser, pl.Message)
	case "agent_message":
		p.add(base, domain.RoleAssistant, pl.Message)
	case "turn_aborted":
		p.add(base, domain.RoleSystem, "Interrupted")
	case "context_compacted":
		p.add(base, domain.RoleSystem, "Conversation compacted")
	}
}

func (p *TranscriptParser) add(m domain.Message, role domain.Role, text string) {
	m.Turn = p.turn
	m.Role = role
	m.Text = strings.TrimSpace(text)
	if m.Text != "" {
		p.log.Add(m)
	}
}

func (p *TranscriptParser) item(pl transcriptPayload, base domain.Message) {
	base.Turn = p.turn
	switch pl.Type {
	case "function_call", "custom_tool_call":
		base.ID = pl.CallID
		base.Role = domain.RoleTool
		base.Tool = &domain.MessageTool{Name: pl.Name, Summary: toolSummary(pl.Name, pl.Arguments, pl.Input), Status: domain.ToolRunning}
		p.log.Add(base)
	case "function_call_output", "custom_tool_call_output":
		status, text := toolOutcome(pl.Output)
		m := domain.ToolResult(pl.CallID, status, text)
		m.Cursor, m.Turn, m.At = base.Cursor, base.Turn, base.At
		p.log.Finish(m)
	case "web_search_call":
		base.Role = domain.RoleTool
		status := domain.ToolDone
		if pl.Status != "" && pl.Status != "completed" {
			status = domain.ToolRunning
		}
		base.Tool = &domain.MessageTool{Name: "web_search", Summary: domain.ToolSummary("Search", pl.Action.Query), Status: status}
		p.log.Add(base)
	}
}

var patchFile = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)

func toolSummary(name, arguments, input string) string {
	var args struct {
		Command json.RawMessage `json:"command"`
		Cmd     string          `json:"cmd"`
		Input   string          `json:"input"`
		Path    string          `json:"path"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	switch name {
	case "shell", "shell_command", "exec_command", "local_shell":
		return domain.ToolSummary("Shell", shellCommand(args.Command, args.Cmd))
	case "apply_patch":
		if input == "" {
			input = args.Input
		}
		var files []string
		for _, m := range patchFile.FindAllStringSubmatch(input, -1) {
			files = append(files, strings.TrimSpace(m[1]))
		}
		return domain.ToolSummary("Edit", strings.Join(files, ", "))
	case "update_plan":
		return "Plan"
	case "view_image":
		return domain.ToolSummary("View", args.Path)
	}
	return name
}

func shellCommand(command json.RawMessage, cmd string) string {
	if cmd != "" {
		return cmd
	}
	var s string
	if json.Unmarshal(command, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(command, &argv) != nil {
		return ""
	}
	if len(argv) == 3 && (argv[1] == "-lc" || argv[1] == "-c") {
		return argv[2]
	}
	return strings.Join(argv, " ")
}

var exitCode = regexp.MustCompile(`(?m)^(?:Exit code:|Process exited with code) (-?\d+)`)

func toolOutcome(raw json.RawMessage) (domain.ToolStatus, string) {
	var out string
	if json.Unmarshal(raw, &out) != nil {
		return domain.ToolDone, ""
	}
	var structured struct {
		Output   string `json:"output"`
		Metadata *struct {
			ExitCode int `json:"exit_code"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(out), &structured) == nil && structured.Metadata != nil {
		if structured.Metadata.ExitCode != 0 {
			return domain.ToolFailed, structured.Output
		}
		return domain.ToolDone, structured.Output
	}
	text := out
	if _, body, ok := strings.Cut(out, "\nOutput:\n"); ok {
		text = body
	}
	if strings.Contains(out, "Process running with session ID") {
		return domain.ToolRunning, text
	}
	if m := exitCode.FindStringSubmatch(out); m != nil && m[1] != "0" {
		return domain.ToolFailed, text
	}
	return domain.ToolDone, text
}
