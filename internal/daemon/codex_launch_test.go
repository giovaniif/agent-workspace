package daemon_test

import (
	"context"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestLaunchTagsACodexSessionWithTheCodexHarness(t *testing.T) {
	host := &fakeHost{}
	_, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}, codex.Adapter{}))
	c := dial(t, path)
	var got domain.Session
	if err := c.Call(context.Background(), rpc.MethodLaunch, rpc.LaunchParams{Harness: "codex", Dir: "/w"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Harness != domain.HarnessCodex {
		t.Fatalf("harness %q", got.Harness)
	}
	if len(host.specs) != 1 || host.specs[0].Command[0] != "codex" {
		t.Fatalf("specs %+v", host.specs)
	}
}
