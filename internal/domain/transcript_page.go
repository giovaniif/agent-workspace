package domain

import "sort"

const (
	TranscriptPageDefault = 50
	TranscriptPageMax     = 500
)

func TranscriptPageLimit(n int) int {
	if n <= 0 {
		return TranscriptPageDefault
	}
	return min(n, TranscriptPageMax)
}

func NewestPage(msgs []Message, limit int) []Message {
	if len(msgs) <= limit {
		return msgs
	}
	i := len(msgs) - limit
	for i > 0 && msgs[i-1].Cursor == msgs[i].Cursor {
		i--
	}
	return msgs[i:]
}

type ToolCalls struct {
	open map[string]Message
}

func (c *ToolCalls) Resolve(msgs []Message) []Message {
	var out []Message
	for _, m := range msgs {
		switch {
		case m.Tool == nil:
			out = append(out, m)
		case m.Tool.Name == "":
			call, ok := c.open[m.ID]
			if !ok {
				continue
			}
			delete(c.open, m.ID)
			tool := *call.Tool
			tool.Status = m.Tool.Status
			call.Tool = &tool
			call.Text = m.Text
			out = append(out, call)
		case m.Tool.Status == ToolRunning && m.ID != "":
			if c.open == nil {
				c.open = map[string]Message{}
			}
			c.open[m.ID] = m
			out = append(out, m)
		default:
			delete(c.open, m.ID)
			out = append(out, m)
		}
	}
	return out
}

func (c *ToolCalls) Open() []string {
	ids := make([]string, 0, len(c.open))
	for id := range c.open {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *ToolCalls) Adopt(other ToolCalls) {
	for id, m := range other.open {
		if c.open == nil {
			c.open = map[string]Message{}
		}
		c.open[id] = m
	}
}
