package serve_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/serve"
)

func TestServePromptReturnsTheDialogAndItsChoices(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodSessionPrompt] = rpc.Prompt{
		ID:      "9f2c4a1b7d3e",
		Text:    "Bash command\n\ntouch notes.txt\n\nDo you want to proceed?",
		Choices: []rpc.PromptChoice{{ID: "1", Label: "Yes"}, {ID: "2", Label: "Yes, and don't ask again"}, {ID: "3", Label: "No"}},
	}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/sessions/s1/prompt", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "prompt.json", body)
	params := f.paramsOf(rpc.MethodSessionPrompt)
	if len(params) != 1 || string(params[0]) != `{"session":"s1"}` {
		t.Fatalf("session.prompt params %s", params)
	}
}

func TestServePromptPassesTheRawPaneOfAnUnrecognizedDialog(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodSessionPrompt] = rpc.Prompt{Choices: []rpc.PromptChoice{}, Raw: "Allow this unusual request?\n  [a] allow  [d] deny"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/sessions/s1/prompt", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	golden(t, "prompt-raw.json", body)
}

func TestServePromptWithNoDialogIsNotFound(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodSessionPrompt] = &rpc.Error{Code: rpc.CodeNotFound, Message: "no permission dialog is showing"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "GET", "/api/v1/sessions/s1/prompt", goodToken, "")
	if status != http.StatusNotFound || errorCode(t, body) != rpc.CodeNotFound {
		t.Fatalf("status %d %s", status, body)
	}
}

func TestServeAnswerCallsSessionAnswerWithTheChoice(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/answer", goodToken, `{"choice":"2"}`)
	if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("status %d %s", status, body)
	}
	params := f.paramsOf(rpc.MethodSessionAnswer)
	if len(params) != 1 || string(params[0]) != `{"session":"s1","choice":"2"}` {
		t.Fatalf("session.answer params %s", params)
	}
}

func TestServeAnswerAfterThePromptWasAnsweredElsewhereIsConflict(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.errs[rpc.MethodSessionAnswer] = &rpc.Error{Code: rpc.CodeStale, Message: "session is no longer waiting for a permission"}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/answer", goodToken, `{"choice":"1"}`)
	if status != http.StatusConflict || errorCode(t, body) != rpc.CodeStale {
		t.Fatalf("status %d %s", status, body)
	}
}

func TestServeAnswerRefusesABodyWithoutAChoice(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	for _, body := range []string{`{}`, `{"choice":""}`, `nope`} {
		status, out := do(t, ts, "POST", "/api/v1/sessions/s1/answer", goodToken, body)
		if status != http.StatusBadRequest || errorCode(t, out) != rpc.CodeBadRequest {
			t.Fatalf("%s: status %d %s", body, status, out)
		}
	}
	if slices.Contains(f.methods(), rpc.MethodSessionAnswer) {
		t.Fatal("a bad body reached session.answer")
	}
}

func TestServeAnswerPassesThePromptItAnswersSoTheDaemonCanRefuseAnotherOne(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, body := do(t, ts, "POST", "/api/v1/sessions/s1/answer", goodToken, `{"choice":"2","prompt":"9f2c4a1b7d3e"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, body)
	}
	params := f.paramsOf(rpc.MethodSessionAnswer)
	if len(params) != 1 || string(params[0]) != `{"session":"s1","choice":"2","prompt":"9f2c4a1b7d3e"}` {
		t.Fatalf("session.answer params %s", params)
	}
}
