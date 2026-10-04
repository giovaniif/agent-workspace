package domain

import (
	"reflect"
	"testing"
)

func running(id string, cursor int64) Message {
	return Message{ID: id, Cursor: cursor, Turn: "t1", Role: RoleTool, Tool: &MessageTool{Name: "Bash", Summary: "Bash go test", Status: ToolRunning}}
}

func finished(id string, cursor int64, status ToolStatus, text string) Message {
	m := running(id, cursor)
	m.Tool = &MessageTool{Name: "Bash", Summary: "Bash go test", Status: status}
	m.Text = text
	return m
}

func bareResult(id string, cursor int64, status ToolStatus, text string) Message {
	m := ToolResult(id, status, text)
	m.Cursor = cursor
	return m
}

func text(id string, cursor int64) Message {
	return Message{ID: id, Cursor: cursor, Role: RoleAssistant, Text: id}
}

func TestToolCallsResolveCompletesBareResultsOfOpenCallsAndDropsTheRest(t *testing.T) {
	cases := []struct {
		name   string
		before [][]Message
		in     []Message
		want   []Message
	}{
		{"plain messages pass", nil, []Message{text("a", 5)}, []Message{text("a", 5)}},
		{"a bare result with no open call is dropped", nil, []Message{text("a", 5), bareResult("c1", 9, ToolDone, "ok")}, []Message{text("a", 5)}},
		{
			"a bare result completes a call seen in an earlier batch, keeping its cursor",
			[][]Message{{running("c1", 5)}},
			[]Message{bareResult("c1", 9, ToolFailed, "exit 1")},
			[]Message{finished("c1", 5, ToolFailed, "exit 1")},
		},
		{
			"a call completes once only",
			[][]Message{{running("c1", 5)}, {bareResult("c1", 9, ToolDone, "ok")}},
			[]Message{bareResult("c1", 12, ToolDone, "again")},
			nil,
		},
		{
			"a finished call closes the open one",
			[][]Message{{running("c1", 5)}, {finished("c1", 5, ToolDone, "ok")}},
			[]Message{bareResult("c1", 12, ToolDone, "late")},
			nil,
		},
		{
			"a running call with no ID is not tracked",
			[][]Message{{running("", 5)}},
			[]Message{bareResult("", 9, ToolDone, "ok")},
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls ToolCalls
			for _, b := range c.before {
				calls.Resolve(b)
			}
			if got := calls.Resolve(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Resolve = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestToolCallsOpenListsCallsStillRunning(t *testing.T) {
	var calls ToolCalls
	calls.Resolve([]Message{running("c1", 5), running("c2", 7), text("a", 8)})
	calls.Resolve([]Message{finished("c1", 5, ToolDone, "ok")})
	if got := calls.Open(); !reflect.DeepEqual(got, []string{"c2"}) {
		t.Fatalf("Open = %v", got)
	}
	var other ToolCalls
	other.Adopt(calls)
	if got := other.Resolve([]Message{bareResult("c2", 20, ToolDone, "ok")}); !reflect.DeepEqual(got, []Message{finished("c2", 7, ToolDone, "ok")}) {
		t.Fatalf("adopted Resolve = %+v", got)
	}
}

func TestNewestPageKeepsTheNewestMessagesWithoutSplittingALine(t *testing.T) {
	msgs := []Message{text("a", 10), text("b", 20), text("c", 30), text("c:1", 30), text("d", 40)}
	cases := []struct {
		name  string
		limit int
		want  []Message
	}{
		{"fewer than the limit", 10, msgs},
		{"exactly the limit", 5, msgs},
		{"the newest", 1, msgs[4:]},
		{"a line's messages stay together", 2, msgs[2:]},
		{"a line boundary is respected", 3, msgs[2:]},
		{"four reaches the line before", 4, msgs[1:]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewestPage(msgs, c.limit); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("NewestPage(%d) = %+v, want %+v", c.limit, got, c.want)
			}
		})
	}
}

func TestTranscriptPageLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, TranscriptPageDefault},
		{-3, TranscriptPageDefault},
		{1, 1},
		{TranscriptPageMax, TranscriptPageMax},
		{TranscriptPageMax + 1, TranscriptPageMax},
	}
	for _, c := range cases {
		if got := TranscriptPageLimit(c.in); got != c.want {
			t.Fatalf("TranscriptPageLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if TranscriptPageDefault != 50 || TranscriptPageMax != 500 {
		t.Fatalf("limits %d %d", TranscriptPageDefault, TranscriptPageMax)
	}
}
