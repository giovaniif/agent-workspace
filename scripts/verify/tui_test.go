package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func upForTUI(t *testing.T, pane string) (*env, *fakeSys) {
	t.Helper()
	e, fs := testEnv(t)
	isoReady(fs)
	fs.paths["tmux"] = true
	fs.paths["freeze"] = true
	fs.handlers["tmux"] = func(c cmd) (string, string, int) {
		joined := strings.Join(c.args, " ")
		switch {
		case strings.Contains(joined, "capture-pane"):
			return pane, "", 0
		case strings.Contains(joined, "has-session"):
			return "", "no server", 1
		}
		return "", "", 0
	}
	fs.handlers["freeze"] = func(c cmd) (string, string, int) {
		for i, a := range c.args {
			if a == "--output" {
				write(t, c.args[i+1], "png")
			}
		}
		return "", "", 0
	}
	if _, code := runJSON(t, e, "iso", "up"); code != 0 {
		t.Fatal("iso up failed")
	}
	return e, fs
}

func TestTUIShotNeedsTheIsolatedCrew(t *testing.T) {
	e, _ := testEnv(t)
	got, code := runJSON(t, e, "tui", "shot", "--out", filepath.Join(t.TempDir(), "a.png"))
	if code != 1 || !strings.Contains(problemText(got), "verify iso up") {
		t.Errorf("exit %d: %s", code, problemText(got))
	}
}

func TestTUIShotStartsTheTUIOnTheIsolatedServerAndRenders(t *testing.T) {
	e, fs := upForTUI(t, "sessions\n")
	out := filepath.Join(t.TempDir(), "a.png")
	got, code := runJSON(t, e, "tui", "shot", "--out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	st, _ := loadIso(e)
	if !fs.ran("tmux -L " + st.Socket + "-tui") {
		t.Error("did not use the isolated TUI tmux server")
	}
	if fs.ran("tmux -L crew ") {
		t.Error("touched the real crew tmux server")
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no shot: %v", err)
	}
	if got["sha256"] == "" {
		t.Error("no hash")
	}
}

func TestTUIDrivePassesWhenTheExpectedTextShows(t *testing.T) {
	e, fs := upForTUI(t, "TEST-alpha  running\n")
	dir := t.TempDir()
	got, code := runJSON(t, e, "tui", "drive", "--out-dir", dir, "--steps", `[{"keys":"j"},{"type":"hi"},{"wait":1},{"expect":"TEST-alpha","timeout":50},{"shot":"after.png"}]`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	if !fs.ran("tmux -L") {
		t.Error("no keys sent")
	}
	sentLiteral := false
	for _, c := range fs.calls {
		j := strings.Join(c.cmd.args, " ")
		if strings.Contains(j, "send-keys") && strings.Contains(j, "-l hi") {
			sentLiteral = true
		}
	}
	if !sentLiteral {
		t.Error("type did not send literal text")
	}
	if _, err := os.Stat(filepath.Join(dir, "after.png")); err != nil {
		t.Error("no shot")
	}
}

func TestTUIDriveFailureSavesThePaneAndTheReason(t *testing.T) {
	e, _ := upForTUI(t, "nothing here\n")
	dir := t.TempDir()
	got, code := runJSON(t, e, "tui", "drive", "--out-dir", dir, "--steps", `[{"expect":"TEST-missing","timeout":30}]`)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(problemText(got), "TEST-missing") {
		t.Errorf("reason %q", problemText(got))
	}
	capture, _ := got["failure_capture"].(string)
	b, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(b), "nothing here") {
		t.Errorf("capture %q: %v %q", capture, err, b)
	}
	if got["failed_step"] != float64(0) {
		t.Errorf("failed_step = %v", got["failed_step"])
	}
}

func TestTUIDriveRejectsUnknownSteps(t *testing.T) {
	e, _ := upForTUI(t, "")
	got, code := runJSON(t, e, "tui", "drive", "--steps", `[{"teleport":"x"}]`)
	if code != 2 || !strings.Contains(problemText(got), "teleport") {
		t.Errorf("exit %d: %s", code, problemText(got))
	}
}
