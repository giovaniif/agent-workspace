package claude_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/domain"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func transcriptFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "transcript", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTranscriptFixtureParsesIntoTheGoldenMessages(t *testing.T) {
	data := transcriptFixture(t)
	messages, end := claude.ParseTranscript(data, 0)
	if end != int64(len(data)) {
		t.Fatalf("end = %d, want %d", end, len(data))
	}
	got, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "transcript", "session.golden.json")
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("messages differ from %s:\n%s", golden, got)
	}
}

func TestTranscriptSkipsAnUnknownEntryAndParsesTheRest(t *testing.T) {
	data := strings.Join([]string{
		`{"type":"user","uuid":"u1","promptId":"p1","message":{"role":"user","content":"fix #42"}}`,
		`{"type":"brand-new-entry","uuid":"x","message":{"role":"user","content":"not a chat line"}}`,
		`{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"brand-new-block","text":"?"},{"type":"text","text":"done"}]}}`,
		`{"type":"user","uuid":"u2","message":{"role":"user","content":{"unexpected":"shape"}}}`,
		`{"type":"system","subtype":"brand-new-subtype","uuid":"s1","content":"ignored"}`,
		``,
	}, "\n")
	messages, _ := claude.ParseTranscript([]byte(data), 0)
	var got []string
	for _, m := range messages {
		got = append(got, string(m.Role)+":"+m.Text)
	}
	want := []string{"user:fix #42", "assistant:done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %q, want %q", got, want)
	}
}

func TestTranscriptHoldsBackATruncatedLastLineUntilItIsComplete(t *testing.T) {
	data := transcriptFixture(t)
	full, _ := claude.ParseTranscript(data, 0)
	lastStart := int64(bytes.LastIndexByte(data[:len(data)-1], '\n') + 1)
	cut := lastStart + 20

	var p claude.TranscriptParser
	head, end := p.Parse(data[:cut], 0)
	if end != lastStart {
		t.Fatalf("end = %d, want the start of the partial line %d", end, lastStart)
	}
	for _, m := range head {
		if m.Cursor > lastStart {
			t.Fatalf("returned %+v from the partial line", m)
		}
	}

	tail, end := p.Parse(data[end:], end)
	if end != int64(len(data)) {
		t.Fatalf("tail end = %d, want %d", end, len(data))
	}
	if got := append(head, tail...); !reflect.DeepEqual(got, full) {
		t.Fatalf("head and tail = %+v\nwant %+v", got, full)
	}
}

func TestTranscriptParserFinishesACallFromAnEarlierChunk(t *testing.T) {
	call := `{"type":"assistant","uuid":"a1","promptId":"p1","cwd":"/work/api","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	result := `{"type":"user","uuid":"u2","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}` + "\n"
	var p claude.TranscriptParser
	first, end := p.Parse([]byte(call), 0)
	second, _ := p.Parse([]byte(result), end)
	want := domain.Message{ID: "toolu_1", Cursor: end, Turn: "p1", Role: domain.RoleTool, Text: "ok", Tool: &domain.MessageTool{Name: "Bash", Summary: "Bash go test ./...", Status: domain.ToolDone}}
	if len(first) != 1 || first[0].Tool.Status != domain.ToolRunning {
		t.Fatalf("first = %+v", first)
	}
	if len(second) != 1 || !reflect.DeepEqual(second[0], want) {
		t.Fatalf("second = %+v\nwant %+v", second, want)
	}
}

func TestTranscriptResultWhoseCallCameBeforeTheCursorIsItsOwnToolMessage(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"assistant","uuid":"a1","promptId":"p1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","uuid":"u2","promptId":"p1","timestamp":"2026-09-29T10:00:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}`,
		``,
	}, "\n"))
	after := int64(bytes.IndexByte(data, '\n') + 1)
	messages, _ := claude.ParseTranscript(data[after:], after)
	want := []domain.Message{{
		ID: "toolu_1", Cursor: int64(len(data)), Turn: "p1", Role: domain.RoleTool, Text: "ok",
		Tool: &domain.MessageTool{Status: domain.ToolDone}, At: messages[0].At,
	}}
	if !reflect.DeepEqual(messages, want) || messages[0].At.IsZero() {
		t.Fatalf("messages = %+v\nwant %+v", messages, want)
	}
}

func TestTranscriptToolResultInANonHumanEntryStillFinishesItsCall(t *testing.T) {
	data := strings.Join([]string{
		`{"type":"assistant","uuid":"a1","promptId":"p1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","uuid":"u2","promptId":"p1","origin":{"kind":"task-notification"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false},{"type":"text","text":"not typed by the user"}]}}`,
		``,
	}, "\n")
	messages, _ := claude.ParseTranscript([]byte(data), 0)
	if len(messages) != 1 || messages[0].Tool.Status != domain.ToolDone || messages[0].Text != "ok" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestTranscriptToolSummaries(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"Edit", `{"file_path":"/work/api/internal/config/load.go","old_string":"a","new_string":"b"}`, "Edit internal/config/load.go"},
		{"Read", `{"file_path":"/etc/hosts"}`, "Read /etc/hosts"},
		{"Write", `{"file_path":"/work/api-old/main.go"}`, "Write /work/api-old/main.go"},
		{"NotebookEdit", `{"notebook_path":"/work/api/nb.ipynb"}`, "NotebookEdit nb.ipynb"},
		{"Bash", `{"command":"/work/api/run.sh --all","description":"Run"}`, "Bash /work/api/run.sh --all"},
		{"Grep", `{"pattern":"func Login","path":"internal"}`, "Grep func Login"},
		{"Glob", `{"pattern":"**/*.go"}`, "Glob **/*.go"},
		{"WebFetch", `{"url":"https://example.com/docs","prompt":"read"}`, "WebFetch https://example.com/docs"},
		{"WebSearch", `{"query":"go fsnotify"}`, "WebSearch go fsnotify"},
		{"Agent", `{"description":"Review web","prompt":"long"}`, "Agent Review web"},
		{"Task", `{"description":"Review api"}`, "Task Review api"},
		{"Skill", `{"skill":"code-review","args":"high"}`, "Skill code-review"},
		{"TodoWrite", `{"todos":[]}`, "TodoWrite"},
		{"Bash", `"not an object"`, "Bash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := `{"type":"assistant","uuid":"a1","cwd":"/work/api","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"` + c.name + `","input":` + c.input + `}]}}` + "\n"
			messages, _ := claude.ParseTranscript([]byte(line), 0)
			if len(messages) != 1 || messages[0].Tool == nil {
				t.Fatalf("messages = %+v", messages)
			}
			if got := messages[0].Tool; got.Summary != c.want || got.Name != c.name || got.Status != domain.ToolRunning {
				t.Fatalf("tool = %+v, want summary %q", got, c.want)
			}
		})
	}
}
