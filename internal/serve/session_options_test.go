package serve_test

import (
	"net/http"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

func TestServeGivesTheNewSessionOptionsWithTheRealDefaults(t *testing.T) {
	f := newFakeDaemon()
	f.devices[goodToken] = phone
	f.results[rpc.MethodSessionOptions] = rpc.SessionOptions{Home: "/home/me", Harnesses: []rpc.HarnessOptions{
		{Harness: "claude", Name: "Claude Code", Tag: "CC", Models: []string{"opus", "sonnet"}, Efforts: []string{"low", "high"},
			DefaultModel: "opus", DefaultEffort: "high"},
	}}
	_, ts := startServer(t, f, serve.Config{URL: publicURL})
	status, out := do(t, ts, "GET", "/api/v1/session-options", goodToken, "")
	if status != http.StatusOK {
		t.Fatalf("status %d %s", status, out)
	}
	golden(t, "session-options.json", out)
}
