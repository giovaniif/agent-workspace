package codex

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func rolloutFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "transcript", "rollout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTranscriptRolloutParsesIntoTheGoldenMessages(t *testing.T) {
	data := rolloutFixture(t)
	messages, end := ParseTranscript(data, 0)
	if end != int64(len(data)) {
		t.Fatalf("end = %d, want %d", end, len(data))
	}
	got, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "transcript", "rollout.golden.json")
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

func TestTranscriptSkipsAnUnknownRolloutEntryAndParsesTheRest(t *testing.T) {
	data := strings.Join([]string{
		`{"timestamp":"2026-09-29T10:00:00Z","type":"event_msg","payload":{"type":"user_message","message":"fix #42"}}`,
		`{"timestamp":"2026-09-29T10:00:01Z","type":"brand_new_line","payload":{"type":"agent_message","message":"not from a known line type"}}`,
		`{"timestamp":"2026-09-29T10:00:02Z","type":"event_msg","payload":{"type":"brand_new_event","message":"?"}}`,
		`{"timestamp":"2026-09-29T10:00:03Z","type":"response_item","payload":{"type":"brand_new_item","call_id":"c"}}`,
		`{"timestamp":"2026-09-29T10:00:04Z","type":"event_msg","payload":"not an object"}`,
		`not json`,
		`{"timestamp":"2026-09-29T10:00:05Z","type":"event_msg","payload":{"type":"agent_message","message":"done"}}`,
		``,
	}, "\n")
	messages, _ := ParseTranscript([]byte(data), 0)
	var got []string
	for _, m := range messages {
		got = append(got, string(m.Role)+":"+m.Text)
	}
	want := []string{"user:fix #42", "assistant:done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %q, want %q", got, want)
	}
}

func TestTranscriptHoldsBackATruncatedRolloutLineUntilItIsComplete(t *testing.T) {
	data := rolloutFixture(t)
	full, _ := ParseTranscript(data, 0)
	lastStart := int64(bytes.LastIndexByte(data[:len(data)-1], '\n') + 1)

	var p TranscriptParser
	head, end := p.Parse(data[:lastStart+10], 0)
	if end != lastStart {
		t.Fatalf("end = %d, want the start of the partial line %d", end, lastStart)
	}
	tail, end := p.Parse(data[end:], end)
	if end != int64(len(data)) {
		t.Fatalf("tail end = %d, want %d", end, len(data))
	}
	if len(tail) != 1 || tail[0].Cursor != int64(len(data)) {
		t.Fatalf("tail = %+v", tail)
	}
	if got := append(head, tail...); !reflect.DeepEqual(got, full) {
		t.Fatalf("head and tail = %+v\nwant %+v", got, full)
	}
}

func TestTranscriptMessageIDsAreStableAcrossParsesFromAnOffset(t *testing.T) {
	data := rolloutFixture(t)
	full, _ := ParseTranscript(data, 0)
	last := full[len(full)-1]
	start := bytes.LastIndexByte(data[:len(data)-1], '\n') + 1
	tail, _ := ParseTranscript(data[start:], int64(start))
	if len(tail) != 1 || tail[0].ID != last.ID || tail[0].Cursor != last.Cursor {
		t.Fatalf("tail = %+v, want ID %q cursor %d", tail, last.ID, last.Cursor)
	}
}

func TestTranscriptToolSummaries(t *testing.T) {
	cases := []struct {
		name, line, want string
	}{
		{"shell_command", `{"type":"function_call","name":"shell_command","arguments":"{\"command\":\"go test ./...\"}","call_id":"c"}`, "Shell go test ./..."},
		{"shell argv", `{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"npm test\"]}","call_id":"c"}`, "Shell npm test"},
		{"shell plain argv", `{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\",\"-la\"]}","call_id":"c"}`, "Shell ls -la"},
		{"exec_command", `{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"rg Login\"}","call_id":"c"}`, "Shell rg Login"},
		{"apply_patch custom", `{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: internal/config/load.go\n@@\n*** Delete File: old.go\n*** End Patch","call_id":"c"}`, "Edit internal/config/load.go, old.go"},
		{"apply_patch function", `{"type":"function_call","name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\\n*** Add File: web/a.ts\\n*** End Patch\"}","call_id":"c"}`, "Edit web/a.ts"},
		{"update_plan", `{"type":"function_call","name":"update_plan","arguments":"{\"plan\":[]}","call_id":"c"}`, "Plan"},
		{"view_image", `{"type":"function_call","name":"view_image","arguments":"{\"path\":\"shot.png\"}","call_id":"c"}`, "View shot.png"},
		{"other", `{"type":"function_call","name":"list_mcp_resources","arguments":"{}","call_id":"c"}`, "list_mcp_resources"},
		{"bad arguments", `{"type":"function_call","name":"shell","arguments":"nope","call_id":"c"}`, "Shell"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := `{"timestamp":"2026-09-29T10:00:00Z","type":"response_item","payload":` + c.line + "}\n"
			messages, _ := ParseTranscript([]byte(line), 0)
			if len(messages) != 1 || messages[0].Tool == nil {
				t.Fatalf("messages = %+v", messages)
			}
			if got := messages[0].Tool; got.Summary != c.want || got.Status != domain.ToolRunning || messages[0].ID != "c" {
				t.Fatalf("tool = %+v, want summary %q", got, c.want)
			}
		})
	}
}

func TestTranscriptToolOutcomes(t *testing.T) {
	cases := []struct {
		name, output string
		status       domain.ToolStatus
		text         string
	}{
		{"exit code zero", `"Exit code: 0\nWall time: 0 seconds\nOutput:\nmain.go\n"`, domain.ToolDone, "main.go"},
		{"exit code nonzero", `"Exit code: 2\nWall time: 1 seconds\nOutput:\nboom\n"`, domain.ToolFailed, "boom"},
		{"exec_command exited", `"Chunk ID: 1\nWall time: 0.1 seconds\nProcess exited with code 1\nOutput:\nFAIL\n"`, domain.ToolFailed, "FAIL"},
		{"exec_command still running", `"Chunk ID: 1\nWall time: 10 seconds\nProcess running with session ID 3\nOutput:\nwatching\n"`, domain.ToolRunning, "watching"},
		{"structured ok", `"{\"output\":\"Success.\\n\",\"metadata\":{\"exit_code\":0}}"`, domain.ToolDone, "Success."},
		{"structured failure", `"{\"output\":\"no such file\",\"metadata\":{\"exit_code\":1}}"`, domain.ToolFailed, "no such file"},
		{"plain text", `"apply_patch verification failed"`, domain.ToolDone, "apply_patch verification failed"},
		{"json without metadata", `"{\"output\":\"x\"}"`, domain.ToolDone, `{"output":"x"}`},
		{"not a string", `[{"type":"input_text","text":"x"}]`, domain.ToolDone, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := `{"timestamp":"2026-09-29T10:00:00Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}","call_id":"c"}}` + "\n" +
				`{"timestamp":"2026-09-29T10:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c","output":` + c.output + `}}` + "\n"
			messages, _ := ParseTranscript([]byte(data), 0)
			if len(messages) != 1 || messages[0].Tool == nil {
				t.Fatalf("messages = %+v", messages)
			}
			if messages[0].Tool.Status != c.status || messages[0].Text != c.text {
				t.Fatalf("status %q text %q, want %q %q", messages[0].Tool.Status, messages[0].Text, c.status, c.text)
			}
		})
	}
}

func TestTranscriptWebSearch(t *testing.T) {
	data := `{"timestamp":"2026-09-29T10:00:00Z","type":"response_item","payload":{"type":"web_search_call","status":"completed","action":{"type":"search","query":"go fsnotify"}}}` + "\n" +
		`{"timestamp":"2026-09-29T10:00:01Z","type":"response_item","payload":{"type":"web_search_call","status":"in_progress","action":{"type":"search","query":"vite pwa"}}}` + "\n"
	messages, _ := ParseTranscript([]byte(data), 0)
	if len(messages) != 2 {
		t.Fatalf("messages = %+v", messages)
	}
	if got := *messages[0].Tool; got != (domain.MessageTool{Name: "web_search", Summary: "Search go fsnotify", Status: domain.ToolDone}) {
		t.Fatalf("first = %+v", got)
	}
	if got := messages[1].Tool.Status; got != domain.ToolRunning {
		t.Fatalf("second status = %q", got)
	}
}
