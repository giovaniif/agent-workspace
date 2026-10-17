package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacEvidenceReadsTheMacOSJobsAndDownloadsSnapshots(t *testing.T) {
	e, fs := testEnv(t)
	fs.paths["gh"] = true
	fs.handlers["gh run"] = func(c cmd) (string, string, int) {
		j := strings.Join(c.args, " ")
		switch {
		case strings.HasPrefix(j, "run view 123 --json"):
			return `{"headSha":"abc","conclusion":"success","url":"https://example.invalid/r/123","jobs":[{"name":"build (macos-latest)","conclusion":"success","url":"u1"},{"name":"tdd","conclusion":"success","url":"u2"},{"name":"macos-app","conclusion":"failure","url":"u3"}]}`, "", 0
		case strings.HasPrefix(j, "run download 123"):
			for i, a := range c.args {
				if a == "--dir" {
					write(t, filepath.Join(c.args[i+1], "macos-snapshots", "main.png"), "img")
				}
			}
			return "", "", 0
		}
		return "", "unexpected", 1
	}
	out := t.TempDir()
	got, code := runJSON(t, e, "mac", "evidence", "--run", "123", "--out-dir", out)
	if code != 1 {
		t.Fatalf("exit %d: a failed macOS job must fail", code)
	}
	jobs := got["jobs"].([]any)
	if len(jobs) != 2 {
		t.Errorf("jobs %v: want only the macOS jobs", jobs)
	}
	if !strings.Contains(problemText(got), "macos-app") {
		t.Errorf("problems %s", problemText(got))
	}
	arts := got["artifacts"].([]any)
	if len(arts) != 1 || arts[0].(map[string]any)["sha256"] == "" {
		t.Errorf("artifacts %v", arts)
	}
	if _, err := os.Stat(arts[0].(map[string]any)["path"].(string)); err != nil {
		t.Error(err)
	}
	if got["head_sha"] != "abc" {
		t.Errorf("head_sha %v", got["head_sha"])
	}
}

func TestMacEvidenceFromAPRUsesItsNewestCIRun(t *testing.T) {
	e, fs := testEnv(t)
	fs.paths["gh"] = true
	fs.handlers["gh pr"] = func(c cmd) (string, string, int) { return `{"headRefOid":"abc"}`, "", 0 }
	fs.handlers["gh run"] = func(c cmd) (string, string, int) {
		j := strings.Join(c.args, " ")
		switch {
		case strings.HasPrefix(j, "run list"):
			if !strings.Contains(j, "--commit abc") {
				t.Errorf("run list %q is not pinned to the head", j)
			}
			return `[{"databaseId":77}]`, "", 0
		case strings.HasPrefix(j, "run view 77"):
			return `{"headSha":"abc","conclusion":"success","jobs":[{"name":"build (macos-latest)","conclusion":"success"}]}`, "", 0
		}
		return "", "", 0
	}
	got := mustOK(t, e, "mac", "evidence", "--pr", "42", "--out-dir", t.TempDir())
	if got["run"] != "77" {
		t.Errorf("run %v", got["run"])
	}
}
