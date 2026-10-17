package main

import (
	"strings"
	"testing"
)

func healthy(fs *fakeSys) {
	for _, p := range []string{"go", "git", "tmux", "node", "freeze"} {
		fs.paths[p] = true
	}
	fs.handlers["node --version"] = func(cmd) (string, string, int) { return "v22.18.0\n", "", 0 }
	fs.handlers["git rev-parse"] = func(c cmd) (string, string, int) {
		if c.args[len(c.args)-1] == "--abbrev-ref" || strings.Contains(strings.Join(c.args, " "), "abbrev-ref") {
			return "main\n", "", 0
		}
		return "abc\n", "", 0
	}
	fs.handlers["git rev-list"] = func(cmd) (string, string, int) { return "0\n", "", 0 }
	fs.handlers["node -e"] = func(cmd) (string, string, int) { return "/fake/chromium\n", "", 0 }
}

func TestDoctorHealthyMachineIsOK(t *testing.T) {
	e, fs := testEnv(t)
	healthy(fs)
	got, code := runJSON(t, e, "doctor")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	if got["branch"] != "main" {
		t.Errorf("branch = %v", got["branch"])
	}
}

func TestDoctorReportsMissingToolsOldNodeAndLoad(t *testing.T) {
	e, fs := testEnv(t)
	healthy(fs)
	delete(fs.paths, "tmux")
	delete(fs.paths, "freeze")
	fs.handlers["node --version"] = func(cmd) (string, string, int) { return "v22.17.9\n", "", 0 }
	fs.loadAvg = 40
	fs.cpus = 8
	got, code := runJSON(t, e, "doctor")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	text := problemText(got)
	for _, want := range []string{"tmux", "freeze", "node v22.17.9", "load"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems %q do not name %q", text, want)
		}
	}
	if !strings.Contains(text, "go install github.com/charmbracelet/freeze@latest") {
		t.Errorf("no install hint for freeze: %q", text)
	}
}

func TestDoctorReportsBehindMainAndBusyPorts(t *testing.T) {
	e, fs := testEnv(t)
	healthy(fs)
	fs.handlers["git rev-list"] = func(cmd) (string, string, int) { return "3\n", "", 0 }
	fs.busy[5199] = true
	got, code := runJSON(t, e, "doctor")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	text := problemText(got)
	if !strings.Contains(text, "3 commits behind origin/main") {
		t.Errorf("problems %q", text)
	}
	if !strings.Contains(text, "5199") {
		t.Errorf("problems %q do not name the busy port", text)
	}
}

func TestNodeVersionAtLeast(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"v22.18.0", true},
		{"v22.17.9", false},
		{"v24.1.0", true},
		{"v21.99.0", false},
		{"junk", false},
	}
	for _, tt := range tests {
		if got := nodeOK(tt.v); got != tt.want {
			t.Errorf("nodeOK(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
