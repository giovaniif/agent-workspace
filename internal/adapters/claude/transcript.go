package claude

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

type transcriptEntry struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	PromptID         string          `json:"promptId"`
	IsMeta           bool            `json:"isMeta"`
	IsSidechain      bool            `json:"isSidechain"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	Content          json.RawMessage `json:"content"`
	Origin           struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
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
	var e transcriptEntry
	if json.Unmarshal(line.Data, &e) != nil || e.IsSidechain {
		return
	}
	if e.PromptID != "" {
		p.turn = e.PromptID
	}
	at, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	base := domain.Message{Cursor: line.End, Turn: p.turn, At: at}
	switch e.Type {
	case "user":
		p.user(e, base)
	case "assistant":
		p.assistant(e, base)
	case "system":
		p.system(e, base)
	}
}

func (p *TranscriptParser) user(e transcriptEntry, base domain.Message) {
	typed := !e.IsMeta && !e.IsCompactSummary && (e.Origin.Kind == "" || e.Origin.Kind == "human")
	var text string
	if json.Unmarshal(e.Message.Content, &text) == nil {
		if typed {
			p.userText(e.UUID, text, base)
		}
		return
	}
	var blocks []contentBlock
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return
	}
	var texts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_result":
			p.toolResult(b, base)
		}
	}
	if typed && len(texts) > 0 {
		p.userText(e.UUID, strings.Join(texts, "\n"), base)
	}
}

var (
	commandName  = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	commandArgs  = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	commandOut   = regexp.MustCompile(`(?s)<local-command-stdout>(.*?)</local-command-stdout>`)
	bashInput    = regexp.MustCompile(`(?s)<bash-input>(.*?)</bash-input>`)
	bashOut      = regexp.MustCompile(`(?s)<bash-stdout>(.*?)</bash-stdout>`)
	bashErr      = regexp.MustCompile(`(?s)<bash-stderr>(.*?)</bash-stderr>`)
	ansiEscape   = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	interruption = "[Request interrupted by user"
)

func (p *TranscriptParser) userText(id, text string, base domain.Message) {
	base.ID = id
	switch {
	case strings.HasPrefix(text, "<local-command-caveat>"), strings.HasPrefix(text, "<task-notification>"):
		return
	case strings.HasPrefix(text, interruption):
		p.systemText(id, "Interrupted", base)
	case strings.HasPrefix(text, "<bash-input>"):
		base.Role = domain.RoleUser
		base.Text = "! " + strings.TrimSpace(firstGroup(bashInput, text))
		p.log.Add(base)
	case strings.HasPrefix(text, "<bash-stdout>"), strings.HasPrefix(text, "<bash-stderr>"):
		out := strings.TrimSpace(firstGroup(bashOut, text) + "\n" + firstGroup(bashErr, text))
		p.systemText(id, out, base)
	case strings.HasPrefix(text, "<command-name>"), strings.HasPrefix(text, "<local-command-stdout>"):
		p.systemText(id, localCommand(text), base)
	default:
		base.Role = domain.RoleUser
		base.Text = strings.TrimSpace(text)
		if base.Text != "" {
			p.log.Add(base)
		}
	}
}

func localCommand(text string) string {
	if name := firstGroup(commandName, text); name != "" {
		return strings.TrimSpace(name + " " + strings.TrimSpace(firstGroup(commandArgs, text)))
	}
	return ansiEscape.ReplaceAllString(firstGroup(commandOut, text), "")
}

func firstGroup(re *regexp.Regexp, text string) string {
	if m := re.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func (p *TranscriptParser) systemText(id, text string, base domain.Message) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	base.ID = id
	base.Role = domain.RoleSystem
	base.Text = text
	p.log.Add(base)
}

func (p *TranscriptParser) assistant(e transcriptEntry, base domain.Message) {
	var blocks []contentBlock
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return
	}
	texts := 0
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			m := base
			m.ID = e.UUID
			if texts > 0 {
				m.ID += ":" + strconv.Itoa(texts)
			}
			texts++
			m.Role = domain.RoleAssistant
			m.Text = strings.TrimSpace(b.Text)
			p.log.Add(m)
		case "tool_use":
			m := base
			m.ID = b.ID
			m.Role = domain.RoleTool
			m.Tool = &domain.MessageTool{Name: b.Name, Summary: toolSummary(b.Name, b.Input, e.Cwd), Status: domain.ToolRunning}
			p.log.Add(m)
		}
	}
}

func (p *TranscriptParser) toolResult(b contentBlock, base domain.Message) {
	status := domain.ToolDone
	if b.IsError {
		status = domain.ToolFailed
	}
	m := domain.ToolResult(b.ToolUseID, status, resultText(b.Content))
	m.Cursor, m.Turn, m.At = base.Cursor, base.Turn, base.At
	p.log.Finish(m)
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func (p *TranscriptParser) system(e transcriptEntry, base domain.Message) {
	var content string
	_ = json.Unmarshal(e.Content, &content)
	switch e.Subtype {
	case "local_command":
		p.systemText(e.UUID, localCommand(content), base)
	case "informational":
		p.systemText(e.UUID, content, base)
	case "compact_boundary":
		p.systemText(e.UUID, "Conversation compacted", base)
	}
}

var toolArgs = map[string]string{
	"Bash":         "command",
	"Read":         "file_path",
	"Write":        "file_path",
	"Edit":         "file_path",
	"MultiEdit":    "file_path",
	"NotebookEdit": "notebook_path",
	"Grep":         "pattern",
	"Glob":         "pattern",
	"WebFetch":     "url",
	"WebSearch":    "query",
	"Agent":        "description",
	"Task":         "description",
	"Skill":        "skill",
}

func toolSummary(name string, input json.RawMessage, cwd string) string {
	var fields map[string]any
	_ = json.Unmarshal(input, &fields)
	key := toolArgs[name]
	arg, _ := fields[key].(string)
	if strings.HasSuffix(key, "_path") {
		arg = relative(arg, cwd)
	}
	return domain.ToolSummary(name, arg)
}

func relative(path, cwd string) string {
	if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(cwd, "/")+"/"); ok && cwd != "" {
		return rest
	}
	return path
}
