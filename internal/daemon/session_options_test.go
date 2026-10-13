package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func sessionOptions(t *testing.T, opts ...daemon.Option) rpc.SessionOptions {
	t.Helper()
	opts = append([]daemon.Option{daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}, codex.Adapter{})}, opts...)
	_, path := start(t, &memStore{}, opts...)
	var got rpc.SessionOptions
	if err := dial(t, path).Call(context.Background(), rpc.MethodSessionOptions, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSessionOptionsListTheHarnessesWithTheirConfiguredDefaults(t *testing.T) {
	t.Setenv("HOME", "/srv/me")
	got := sessionOptions(t,
		daemon.WithStartDefaults(map[domain.Harness]daemon.StartDefaults{domain.HarnessCodex: {Model: "gpt-6-sol", Effort: "high"}}))
	claudeSpec, codexSpec := domain.Spec(domain.HarnessClaude), domain.Spec(domain.HarnessCodex)
	want := rpc.SessionOptions{
		Harnesses: []rpc.HarnessOptions{
			{Harness: "claude", Name: claudeSpec.Name, Tag: claudeSpec.Tag, Models: claudeSpec.Models, Efforts: claudeSpec.Efforts},
			{Harness: "codex", Name: codexSpec.Name, Tag: codexSpec.Tag, Models: codexSpec.Models, Efforts: codexSpec.Efforts, Model: "gpt-6-sol", Effort: "high"},
		},
		Home: "/srv/me",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestSessionOptionsNameTheServersHomeForTypedPaths(t *testing.T) {
	t.Setenv("HOME", "/srv/me")
	if got := sessionOptions(t).Home; got != "/srv/me" {
		t.Fatalf("home = %q, want /srv/me", got)
	}
}

func TestSessionOptionsHaveNoLauncherLimit(t *testing.T) {
	_, path := start(t, &memStore{}, daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}))
	var raw map[string]json.RawMessage
	if err := dial(t, path).Call(context.Background(), rpc.MethodSessionOptions, struct{}{}, &raw); err != nil {
		t.Fatal(err)
	}
	if _, has := raw["max_parallel"]; has {
		t.Fatalf("session.options still sends max_parallel: %v", raw)
	}
}

func TestStartDefaultsComeFromTheDefaultsTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if got, err := daemon.LoadStartDefaults(path); err != nil || len(got) != 0 {
		t.Fatalf("missing file: %v, %v", got, err)
	}
	body := "[defaults.claude]\nmodel = \"opus\"\neffort = \"xhigh\"\n[defaults.codex]\nmodel = \"gpt-6-sol\"\n[defaults.nope]\nmodel = \"x\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := daemon.LoadStartDefaults(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.Harness]daemon.StartDefaults{
		domain.HarnessClaude: {Model: "opus", Effort: "xhigh"},
		domain.HarnessCodex:  {Model: "gpt-6-sol"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := os.WriteFile(path, []byte("[defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.LoadStartDefaults(path); err == nil {
		t.Fatal("a broken file gave no error")
	}
}
