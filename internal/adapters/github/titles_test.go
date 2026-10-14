package github_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/github"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

func fakeGH(t *testing.T, script string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "gh")
	argsFile = filepath.Join(dir, "args")
	body := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n" + script + "\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func TestNamingPRTitleFromGH(t *testing.T) {
	bin, argsFile := fakeGH(t, `echo '{"title":"  Add login "}'`)
	task := domain.Task{Source: domain.TaskPR, Ref: "api#12", URL: "https://github.com/o/api/pull/12"}
	title, err := github.Titles{Bin: bin}.Title(context.Background(), task)
	if err != nil || title != "Add login" {
		t.Fatalf("Title = %q, %v", title, err)
	}
	args, _ := os.ReadFile(argsFile)
	if got := strings.TrimSpace(string(args)); got != "pr view https://github.com/o/api/pull/12 --json title" {
		t.Errorf("gh args = %q", got)
	}
}

func TestNamingPRTitleSkipsOtherTasks(t *testing.T) {
	bin, argsFile := fakeGH(t, `echo '{"title":"x"}'`)
	for _, task := range []domain.Task{
		{Source: domain.TaskText, Text: "https://github.com/o/api/pull/12"},
		{Source: domain.TaskText, Text: "x"},
		{Source: domain.TaskPR},
	} {
		title, err := github.Titles{Bin: bin}.Title(context.Background(), task)
		if title != "" || err != nil {
			t.Errorf("Title(%+v) = %q, %v; want nothing", task, title, err)
		}
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Error("gh ran for a task that is not a PR")
	}
}

func TestNamingPRTitleFailures(t *testing.T) {
	task := domain.Task{Source: domain.TaskPR, URL: "https://github.com/o/api/pull/12"}
	failing, _ := fakeGH(t, `exit 1`)
	garbled, _ := fakeGH(t, `echo nope`)
	for name, bin := range map[string]string{"gh fails": failing, "not json": garbled, "gh missing": filepath.Join(t.TempDir(), "none")} {
		if title, err := (github.Titles{Bin: bin}).Title(context.Background(), task); err == nil || title != "" {
			t.Errorf("%s: Title = %q, %v; want an error", name, title, err)
		}
	}
}
