package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestSetupClaudeMergesIntoClaudeConfigDirAndRemoveUndoesIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	settings := filepath.Join(dir, "settings.json")
	original := []byte(`{"theme": "dark"}` + "\n")
	if err := os.WriteFile(settings, original, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"setup", "claude"}, &out, &errOut); code != 0 {
		t.Fatalf("setup: code %d, %s", code, errOut.String())
	}
	merged, _ := os.ReadFile(settings)
	if !strings.Contains(string(merged), " hook --harness claude --event Stop") || !strings.Contains(out.String(), settings) {
		t.Fatalf("settings:\n%s\nstdout: %s", merged, out.String())
	}
	if code := run([]string{"setup", "claude", "--remove"}, &out, &errOut); code != 0 {
		t.Fatalf("remove: code %d, %s", code, errOut.String())
	}
	if got, _ := os.ReadFile(settings); !bytes.Equal(got, original) {
		t.Fatalf("after remove:\n%s", got)
	}
}

func TestSetupNeedsAKnownHarness(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"setup", "vim"}, &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
}

const statusJSON = `{"model":{"display_name":"Opus 5.5"},"effort":{"level":"high"},"context_window":{"remaining_percentage":70},"rate_limits":{"five_hour":{"used_percentage":20,"resets_at":1}}}`

func TestStatusLineChainsTheUsersCommandAndReportsToTheDaemon(t *testing.T) {
	home := shortHome(t)
	got := make(chan rpc.Request, 1)
	fakeDaemon(t, home, func(req rpc.Request) *rpc.Response { got <- req; return nil })
	var out bytes.Buffer
	code := runStatusLine([]string{"--chain", `printf 'mine: '; cat | wc -c | tr -d ' '`}, strings.NewReader(statusJSON), &out, home, "%3")
	if code != 0 || out.String() != "mine: "+itoa(len(statusJSON))+"\n" {
		t.Fatalf("code %d, stdout %q", code, out.String())
	}
	var req rpc.Request
	select {
	case req = <-got:
	case <-time.After(time.Second):
		t.Fatal("nothing reached the daemon")
	}
	var sl rpc.StatusLine
	if err := json.Unmarshal(req.Params, &sl); err != nil {
		t.Fatal(err)
	}
	want := domain.StatusReport{Model: "Opus 5.5", Effort: "high", ContextLeft: 70, HasContext: true,
		Limits: []domain.RateLimit{{Window: "five_hour", UsedPercent: 20, ResetsAt: 1}}}
	if req.Method != rpc.MethodStatusLine || sl.Pane != "%3" || sl.Report.Model != want.Model || sl.Report.ContextLeft != 70 || len(sl.Report.Limits) != 1 {
		t.Fatalf("request %+v, status line %+v", req, sl)
	}
}

func TestStatusLineWithTheDaemonDownStillPrintsTheUsersOutput(t *testing.T) {
	home := shortHome(t)
	var out bytes.Buffer
	start := time.Now()
	code := runStatusLine([]string{"--chain", "echo mine"}, strings.NewReader(statusJSON), &out, home, "%3")
	if code != 0 || out.String() != "mine\n" {
		t.Fatalf("code %d, stdout %q", code, out.String())
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("took %v", took)
	}
	if _, err := os.Stat(filepath.Join(home, "hook.log")); errors.Is(err, os.ErrNotExist) {
		t.Fatal("no log line")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSetupClaudeAlsoInstallsTheBrowserSkillAndRemoveTakesItAway(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	file := filepath.Join(dir, "skills", "agentws-browser", "SKILL.md")
	var out, errOut bytes.Buffer
	if code := run([]string{"setup", "claude"}, &out, &errOut); code != 0 {
		t.Fatalf("setup: code %d, %s", code, errOut.String())
	}
	if body, err := os.ReadFile(file); err != nil || !strings.Contains(string(body), "crew browser snapshot") || !strings.Contains(out.String(), file) {
		t.Fatalf("skill: %v, stdout %q", err, out.String())
	}
	if code := run([]string{"setup", "claude", "--remove"}, &out, &errOut); code != 0 {
		t.Fatalf("remove: code %d, %s", code, errOut.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("skill left: %v", err)
	}
}
