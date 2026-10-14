package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupOmp(t *testing.T, agentDir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := func(k string) string {
		if k == "PI_CODING_AGENT_DIR" {
			return agentDir
		}
		return ""
	}
	code := runSetup(append([]string{"omp"}, args...), &out, &errOut, env, "/opt/agentws/bin/agentws")
	return code, out.String(), errOut.String()
}

func TestSetupOmpWritesTheHookFileOnce(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hooks", "post", "agentws.ts")
	code, out, errOut := setupOmp(t, dir)
	if code != 0 || errOut != "" || out != "wrote the agentws hook file "+file+"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	body, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(body), `"/opt/agentws/bin/agentws"`) {
		t.Fatalf("hook file %q, %v", body, err)
	}
	if code, out, _ := setupOmp(t, dir); code != 0 || out != "already set up in "+file+"\n" {
		t.Fatalf("second run: code %d, stdout %q", code, out)
	}
}

func TestSetupOmpRemoveDeletesOnlyOurFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hooks", "post", "agentws.ts")
	setupOmp(t, dir)
	if code, out, _ := setupOmp(t, dir, "--remove"); code != 0 || out != "removed the agentws hook file "+file+"\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("hook file left behind: %v", err)
	}
	if _, out, _ := setupOmp(t, dir, "--remove"); out != "nothing to remove in "+file+"\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestSetupOmpLeavesAHookFileOfTheUsers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hooks", "post", "agentws.ts")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("export default () => {};\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := setupOmp(t, dir)
	if code != 1 || !strings.Contains(errOut, file) {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if body, _ := os.ReadFile(file); string(body) != "export default () => {};\n" {
		t.Fatalf("hook file changed: %q", body)
	}
	if code, _, _ := setupOmp(t, dir, "--remove"); code != 1 {
		t.Fatalf("remove code %d", code)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("remove deleted the user's file: %v", err)
	}
}
