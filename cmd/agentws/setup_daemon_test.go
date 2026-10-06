package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeTool(t *testing.T, bin, name, body string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), name+".log")
	script := "#!/bin/sh\necho \"$@\" >> '" + log + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

type daemonSetupFixture struct {
	home    string
	path    string
	shell   string
	manager string
	bin     string
}

func newDaemonSetupFixture(t *testing.T, goos, linger string) *daemonSetupFixture {
	t.Helper()
	f := &daemonSetupFixture{home: t.TempDir(), bin: t.TempDir()}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CONFIG_HOME", "")
	f.shell = filepath.Join(f.bin, "loginsh")
	if err := os.WriteFile(f.shell, []byte("#!/bin/sh\n[ \"$1\" = -l ] && PATH=/login/bin:/usr/bin exec /bin/sh -c \"$3\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if goos == "darwin" {
		f.manager = fakeTool(t, f.bin, "launchctl", "exit 0")
		f.path = filepath.Join(f.home, "Library", "LaunchAgents", "dev.agentws.daemon.plist")
	} else {
		f.manager = fakeTool(t, f.bin, "systemctl", "exit 0")
		f.path = filepath.Join(f.home, ".config", "systemd", "user", "agentws-daemon.service")
	}
	fakeTool(t, f.bin, "loginctl", "echo Linger="+linger)
	t.Setenv("PATH", f.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func (f *daemonSetupFixture) run(t *testing.T, goos string, agentwsHome string, args ...string) (int, string, string) {
	t.Helper()
	env := envOf(map[string]string{"AGENTWS_HOME": agentwsHome, "PATH": "/caller/bin:" + f.bin, "SHELL": f.shell, "USER": "me"})
	var stdout, stderr bytes.Buffer
	code := runSetupDaemon(args, &stdout, &stderr, env, "/bin/agentws", goos)
	return code, stdout.String(), stderr.String()
}

func daemonLifecycle(t *testing.T, goos string) {
	f := newDaemonSetupFixture(t, goos, "yes")
	home := filepath.Join(t.TempDir(), "home")

	code, out, errOut := f.run(t, goos, home)
	if code != 0 || !strings.Contains(out, "wrote and started "+f.path) {
		t.Fatalf("install: code %d out %q err %q", code, out, errOut)
	}
	first, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/bin/agentws", "daemon", home, "/login/bin:/usr/bin"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("missing %q in\n%s", want, first)
		}
	}
	if strings.Contains(string(first), "/caller/bin") {
		t.Fatalf("the caller's PATH leaked into\n%s", first)
	}

	code, out, _ = f.run(t, goos, home)
	if code != 0 || !strings.Contains(out, "already set up in "+f.path) {
		t.Fatalf("reinstall: code %d out %q", code, out)
	}
	if after, _ := os.ReadFile(f.path); !bytes.Equal(first, after) {
		t.Fatal("running it twice rewrote the file")
	}

	other := filepath.Join(t.TempDir(), "other")
	code, out, _ = f.run(t, goos, other)
	if code != 0 || !strings.Contains(out, "previous file saved as "+f.path+".bak") {
		t.Fatalf("change: code %d out %q", code, out)
	}
	if old, _ := os.ReadFile(f.path + ".bak"); !bytes.Equal(old, first) {
		t.Fatal("the backup is not the previous file")
	}

	_ = os.Remove(f.manager)
	code, out, _ = f.run(t, goos, other, "--remove")
	if code != 0 || !strings.Contains(out, "stopped and removed") {
		t.Fatalf("remove: code %d out %q", code, out)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("service file still there: %v", err)
	}
	calls, _ := os.ReadFile(f.manager)
	if goos == "linux" && !strings.Contains(string(calls), "disable --now agentws-daemon.service") {
		t.Fatalf("remove did not stop the unit: %q", calls)
	}
	if goos == "darwin" && !strings.Contains(string(calls), "bootout") {
		t.Fatalf("remove did not stop the agent: %q", calls)
	}

	code, out, _ = f.run(t, goos, other, "--remove")
	if code != 0 || !strings.Contains(out, "nothing to remove") {
		t.Fatalf("second remove: code %d out %q", code, out)
	}
}

func TestSetupDaemonLifecycleOnSystemd(t *testing.T) { daemonLifecycle(t, "linux") }

func TestSetupDaemonLifecycleOnLaunchd(t *testing.T) { daemonLifecycle(t, "darwin") }

func TestSetupDaemonLingerOffIsReportedWithTheFix(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "no")
	code, out, _ := f.run(t, "linux", t.TempDir())
	if code != 0 || !strings.Contains(out, "stop at logout") || !strings.Contains(out, "sudo loginctl enable-linger me") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestSetupDaemonLingerOnIsQuiet(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "yes")
	_, out, _ := f.run(t, "linux", t.TempDir())
	if strings.Contains(out, "linger") {
		t.Fatalf("out %q", out)
	}
}

func TestSetupDaemonCheckPrintsJSON(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "no")
	home := t.TempDir()
	check := func() map[string]bool {
		code, out, errOut := f.run(t, "linux", home, "--check")
		if code != 0 {
			t.Fatalf("code %d err %q", code, errOut)
		}
		var got map[string]bool
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%q: %v", out, err)
		}
		return got
	}
	if got := check(); got["installed"] || !got["running"] || got["linger"] {
		t.Fatalf("before install %v", got)
	}
	f.run(t, "linux", home)
	if got := check(); !got["installed"] || !got["running"] || got["linger"] {
		t.Fatalf("after install %v", got)
	}
	fakeTool(t, f.bin, "systemctl", "exit 3")
	fakeTool(t, f.bin, "loginctl", "echo Linger=yes")
	if got := check(); !got["installed"] || got["running"] || !got["linger"] {
		t.Fatalf("stopped with linger %v", got)
	}
}

func TestSetupDaemonCheckFailsWhenLingerCannotBeRead(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "yes")
	fakeTool(t, f.bin, "loginctl", "exit 1")
	if code, out, _ := f.run(t, "linux", t.TempDir(), "--check"); code != 1 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestSetupDaemonUnknownLingerIsNotReportedAsOff(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "yes")
	fakeTool(t, f.bin, "loginctl", "exit 1")
	code, out, errOut := f.run(t, "linux", t.TempDir())
	if code != 0 || strings.Contains(out, "enable-linger") || !strings.Contains(errOut, "could not check linger") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSetupDaemonCheckFailsWhenTheServiceFileCannotBeInspected(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads any directory")
	}
	f := newDaemonSetupFixture(t, "linux", "yes")
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if code, out, _ := f.run(t, "linux", t.TempDir(), "--check"); code != 1 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestSetupDaemonRefusesArguments(t *testing.T) {
	f := newDaemonSetupFixture(t, "linux", "yes")
	for _, args := range [][]string{{"extra"}, {"--bogus"}, {"--check", "--remove"}} {
		if code, _, errOut := f.run(t, "linux", t.TempDir(), args...); code != 2 || !strings.Contains(errOut, "usage: agentws setup daemon") {
			t.Errorf("%q: code %d err %q", args, code, errOut)
		}
	}
	if code, _, _ := f.run(t, "windows", t.TempDir()); code != 2 {
		t.Errorf("windows: code %d", code)
	}
}

func TestSetupDaemonIsReachableFromSetup(t *testing.T) {
	var stderr bytes.Buffer
	if code := runSetup([]string{"daemon", "extra"}, &bytes.Buffer{}, &stderr, envOf(nil), "/bin/agentws"); code != 2 || !strings.Contains(stderr.String(), "usage: agentws setup daemon") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}
