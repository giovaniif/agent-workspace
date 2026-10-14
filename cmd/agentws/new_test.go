package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestNewArgsBecomeSessionParams(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseNewArgs([]string{"--workspace", "shop", "--harness", "codex", "--model", "m", "--effort", "high", "fix", "the build"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.NewSessionParams{Workspace: filepath.Join(cwd, "shop"), WorkItem: "fix the build", Harness: "codex", Model: "m", Effort: "high"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	got, err = parseNewArgs([]string{"https://github.com/acme/api/pull/1"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want := (rpc.NewSessionParams{WorkItem: "https://github.com/acme/api/pull/1", Harness: "claude"}); got != want {
		t.Fatalf("defaults: got %+v, want %+v", got, want)
	}
}

func TestNewWithoutAWorkItemIsAUsageError(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"new", "--harness", "codex"}, io.Discard, &stderr); code != 2 || !bytes.Contains(stderr.Bytes(), []byte("usage: agentws new")) {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}
