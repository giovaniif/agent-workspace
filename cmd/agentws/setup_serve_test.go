package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestSetupServeUnitRunsServeWithThisShellsEnvironment(t *testing.T) {
	env := envOf(map[string]string{"PATH": "/usr/local/bin:/usr/bin", "AGENTWS_HOME": "/home/me/.agentws"})
	svc, remove, err := setupServeService([]string{"--addr", "0.0.0.0:7420", "--cert", "/c/m.crt", "--key", "/c/m.key"}, env, "/usr/local/bin/agentws", "/home/me", 1000, "linux")
	if err != nil || remove {
		t.Fatalf("remove %v err %v", remove, err)
	}
	u := svc.unit
	if u.Name != "agentws-serve" || u.Dir != "/home/me/.config/systemd/user" || u.Log != "/home/me/.agentws/serve.log" {
		t.Fatalf("unit %+v", u)
	}
	if want := []string{"/usr/local/bin/agentws", "serve", "--addr", "0.0.0.0:7420", "--cert", "/c/m.crt", "--key", "/c/m.key"}; !reflect.DeepEqual(u.Program, want) {
		t.Fatalf("program %q", u.Program)
	}
	if want := map[string]string{"PATH": "/usr/local/bin:/usr/bin", "AGENTWS_HOME": "/home/me/.agentws"}; !reflect.DeepEqual(u.Env, want) {
		t.Fatalf("env %v", u.Env)
	}
}

func TestSetupServeUnitHonoursXDGConfigHome(t *testing.T) {
	svc, _, err := setupServeService(nil, envOf(map[string]string{"XDG_CONFIG_HOME": "/xdg"}), "/bin/agentws", "/home/me", 1000, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if svc.unit.Dir != "/xdg/systemd/user" {
		t.Fatalf("dir %q", svc.unit.Dir)
	}
	if want := []string{"/bin/agentws", "serve"}; !reflect.DeepEqual(svc.unit.Program, want) {
		t.Fatalf("program %q", svc.unit.Program)
	}
	if svc.unit.Env["AGENTWS_HOME"] != "/home/me/.agentws" {
		t.Fatalf("env %v", svc.unit.Env)
	}
}

func TestSetupServeAgentOnMacOSIsALaunchdAgent(t *testing.T) {
	env := envOf(map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"})
	svc, _, err := setupServeService([]string{"--self-signed"}, env, "/opt/homebrew/bin/agentws", "/Users/me", 501, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	a := svc.agent
	if !svc.launchd || a.Label != "dev.agentws.serve" || a.Dir != "/Users/me/Library/LaunchAgents" || a.UID != 501 || a.Log != "/Users/me/.agentws/serve.log" {
		t.Fatalf("service %+v", svc)
	}
	if want := []string{"/opt/homebrew/bin/agentws", "serve", "--self-signed"}; !reflect.DeepEqual(a.Program, want) {
		t.Fatalf("program %q", a.Program)
	}
}

func TestSetupServeWritesFlagsInOneOrderSoReorderedArgsChangeNothing(t *testing.T) {
	env := envOf(nil)
	a, _, _ := setupServeService([]string{"--key", "/k", "--cert", "/c", "--addr", "[::]:7420"}, env, "/bin/agentws", "/h", 1, "linux")
	b, _, _ := setupServeService([]string{"--addr", "[::]:7420", "--cert", "/c", "--key", "/k"}, env, "/bin/agentws", "/h", 1, "linux")
	if !reflect.DeepEqual(a.unit.Program, b.unit.Program) {
		t.Fatalf("%q vs %q", a.unit.Program, b.unit.Program)
	}
}

func TestSetupServeMakesCertPathsAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	svc, _, err := setupServeService([]string{"--cert", "certs/m.crt", "--key", "certs/m.key"}, envOf(nil), "/bin/agentws", "/h", 1, "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/bin/agentws", "serve", "--cert", filepath.Join(wd, "certs/m.crt"), "--key", filepath.Join(wd, "certs/m.key")}
	if !reflect.DeepEqual(svc.unit.Program, want) {
		t.Fatalf("program %q", svc.unit.Program)
	}
}

func TestSetupServeRemoveNamesTheSameService(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		installed, _, _ := setupServeService([]string{"--self-signed"}, envOf(nil), "/bin/agentws", "/h", 1, goos)
		removed, remove, err := setupServeService([]string{"--remove"}, envOf(nil), "/bin/agentws", "/h", 1, goos)
		if err != nil || !remove || removed.unit.Name != installed.unit.Name || removed.agent.Label != installed.agent.Label {
			t.Fatalf("%s: %+v remove %v err %v", goos, removed, remove, err)
		}
	}
}

func TestSetupServeRefusesWhatServeWouldRefuse(t *testing.T) {
	for name, args := range map[string][]string{
		"cert without key":           {"--cert", "/c"},
		"key without cert":           {"--key", "/k"},
		"self-signed with cert":      {"--self-signed", "--cert", "/c", "--key", "/k"},
		"plain http off loopback":    {"--addr", "0.0.0.0:7420"},
		"plain http on a hostname":   {"--addr", "machine.example.com:7420"},
		"an unknown flag":            {"--tls"},
		"a positional argument":      {"extra"},
		"an address without a port":  {"--addr", "127.0.0.1"},
		"an empty address":           {"--addr", ""},
		"a cert path that is empty":  {"--cert", "", "--key", "/k"},
		"a key path that is empty":   {"--cert", "/c", "--key", ""},
		"self-signed with a key":     {"--self-signed", "--key", "/k"},
		"an address with a bad port": {"--addr", "127.0.0.1:http"},
	} {
		if _, _, err := setupServeService(args, envOf(nil), "/bin/agentws", "/h", 1, "linux"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestSetupServeAcceptsLoopbackWithoutTLSAndAnyAddressWithIt(t *testing.T) {
	for _, args := range [][]string{
		{"--addr", "127.0.0.1:9000"},
		{"--addr", "localhost:9000"},
		{"--addr", "[::1]:9000"},
		{"--addr", "0.0.0.0:7420", "--self-signed"},
		{"--addr", "machine.tailnet-name.ts.net:7420", "--cert", "/c", "--key", "/k"},
		{"--remove"},
		{"--remove", "--addr", "0.0.0.0:7420"},
	} {
		if _, _, err := setupServeService(args, envOf(nil), "/bin/agentws", "/h", 1, "linux"); err != nil {
			t.Errorf("%q: %v", args, err)
		}
	}
}

func TestSetupServeIsLinuxAndMacOSOnly(t *testing.T) {
	if _, _, err := setupServeService(nil, envOf(nil), "/bin/agentws", "/h", 1, "windows"); err == nil {
		t.Fatal("windows was accepted")
	}
}

func TestSetupServeUsageOnBadArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := runSetup([]string{"serve", "--cert", "/c"}, &bytes.Buffer{}, &stderr, envOf(nil), "/bin/agentws"); code != 2 || !strings.Contains(stderr.String(), "usage: agentws setup serve") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

type fakeServiceManager struct {
	log string
}

func newFakeServiceManager(t *testing.T, name string) *fakeServiceManager {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), name+".log")
	script := "#!/bin/sh\necho \"$@\" >> '" + log + "'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &fakeServiceManager{log: log}
}

func (f *fakeServiceManager) calls(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(f.log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (f *fakeServiceManager) reset() { _ = os.Remove(f.log) }

func serveLifecycle(t *testing.T, goos, servicePath string, manager *fakeServiceManager) {
	t.Helper()
	env := envOf(map[string]string{"AGENTWS_HOME": filepath.Join(t.TempDir(), "home"), "PATH": os.Getenv("PATH")})
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := runSetupServe(args, &stdout, &stderr, env, "/bin/agentws", goos)
		return code, stdout.String(), stderr.String()
	}

	code, out, errOut := run("--self-signed")
	if code != 0 || !strings.Contains(out, "wrote and started "+servicePath) {
		t.Fatalf("install: code %d out %q err %q", code, out, errOut)
	}
	if len(manager.calls(t)) == 0 {
		t.Fatal("the service manager was never called")
	}
	first, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}

	manager.reset()
	code, out, _ = run("--self-signed")
	if code != 0 || !strings.Contains(out, "already set up in "+servicePath) {
		t.Fatalf("reinstall: code %d out %q", code, out)
	}
	if after, _ := os.ReadFile(servicePath); !bytes.Equal(first, after) {
		t.Fatal("the same arguments rewrote the file")
	}

	manager.reset()
	code, out, _ = run("--addr", "127.0.0.1:9000")
	if code != 0 || !strings.Contains(out, "previous file saved as "+servicePath+".bak") {
		t.Fatalf("change: code %d out %q", code, out)
	}
	if old, _ := os.ReadFile(servicePath + ".bak"); !bytes.Equal(old, first) {
		t.Fatal("the backup is not the previous file")
	}
	if now, _ := os.ReadFile(servicePath); !strings.Contains(string(now), "127.0.0.1:9000") {
		t.Fatalf("the new arguments were not written:\n%s", now)
	}

	manager.reset()
	code, out, _ = run("--remove")
	if code != 0 || !strings.Contains(out, "stopped and removed") {
		t.Fatalf("remove: code %d out %q", code, out)
	}
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		t.Fatalf("service file still there: %v", err)
	}

	code, out, _ = run("--remove")
	if code != 0 || !strings.Contains(out, "nothing to remove") {
		t.Fatalf("second remove: code %d out %q", code, out)
	}
}

func TestSetupServeLifecycleOnSystemd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	manager := newFakeServiceManager(t, "systemctl")
	serveLifecycle(t, "linux", filepath.Join(home, ".config", "systemd", "user", "agentws-serve.service"), manager)
}

func TestSetupServeLifecycleOnLaunchd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	manager := newFakeServiceManager(t, "launchctl")
	serveLifecycle(t, "darwin", filepath.Join(home, "Library", "LaunchAgents", "dev.agentws.serve.plist"), manager)
}
