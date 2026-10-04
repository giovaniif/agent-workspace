package systemd_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/systemd"
)

type systemctl struct {
	calls [][]string
	fail  map[string]bool
}

func (s *systemctl) run(_ context.Context, name string, args ...string) error {
	s.calls = append(s.calls, append([]string{name}, args...))
	verb := args[1]
	if s.fail[verb] {
		return errors.New("systemctl " + verb + " failed")
	}
	return nil
}

func unit(dir string) systemd.Unit {
	return systemd.Unit{
		Dir:     dir,
		Name:    "agentws-serve",
		Program: []string{"/usr/local/bin/agentws", "serve", "--addr", "0.0.0.0:7420", "--cert", "/home/me/my certs/m.crt"},
		Env:     map[string]string{"PATH": "/usr/local/bin:/usr/bin", "AGENTWS_HOME": "/home/me/.agentws"},
		Log:     "/home/me/.agentws/serve.log",
	}
}

func TestSystemdInstallWritesARestartedUserUnitAndStartsIt(t *testing.T) {
	dir := t.TempDir()
	s := &systemctl{}
	res, err := systemd.Install(context.Background(), unit(dir), s.run)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agentws-serve.service")
	if !res.Changed || res.Path != path || res.Backup != "" {
		t.Fatalf("result %+v", res)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=\"/usr/local/bin/agentws\" \"serve\" \"--addr\" \"0.0.0.0:7420\" \"--cert\" \"/home/me/my certs/m.crt\"\n",
		"Environment=\"AGENTWS_HOME=/home/me/.agentws\"\nEnvironment=\"PATH=/usr/local/bin:/usr/bin\"\n",
		"Restart=always\n",
		"StandardOutput=append:/home/me/.agentws/serve.log\n",
		"StandardError=append:/home/me/.agentws/serve.log\n",
		"WantedBy=default.target\n",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("unit lacks %q:\n%s", want, raw)
		}
	}
	want := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "agentws-serve.service"},
		{"systemctl", "--user", "restart", "agentws-serve.service"},
	}
	if !reflect.DeepEqual(s.calls, want) {
		t.Fatalf("systemctl calls %q", s.calls)
	}
}

func TestSystemdInstallEscapesWhatSystemdExpands(t *testing.T) {
	dir := t.TempDir()
	u := unit(dir)
	u.Program = []string{"/opt/a%b/agentws", "$HOME", `say "hi"\`}
	u.Env = map[string]string{"X": "100%"}
	if _, err := systemd.Install(context.Background(), u, (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "agentws-serve.service"))
	for _, want := range []string{`ExecStart="/opt/a%%b/agentws" "$$HOME" "say \"hi\"\\"`, `Environment="X=100%%"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("unit lacks %q:\n%s", want, raw)
		}
	}
}

func TestSystemdInstallTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := systemd.Install(context.Background(), unit(dir), (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	s := &systemctl{}
	res, err := systemd.Install(context.Background(), unit(dir), s.run)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"systemctl", "--user", "is-active", "agentws-serve.service"}}; res.Changed || !reflect.DeepEqual(s.calls, want) {
		t.Fatalf("result %+v calls %q", res, s.calls)
	}
}

func TestSystemdInstallStartsAnUnchangedUnitThatIsNotRunning(t *testing.T) {
	dir := t.TempDir()
	if _, err := systemd.Install(context.Background(), unit(dir), (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	s := &systemctl{fail: map[string]bool{"is-active": true}}
	res, err := systemd.Install(context.Background(), unit(dir), s.run)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"systemctl", "--user", "is-active", "agentws-serve.service"},
		{"systemctl", "--user", "enable", "agentws-serve.service"},
		{"systemctl", "--user", "restart", "agentws-serve.service"},
	}
	if res.Changed || !reflect.DeepEqual(s.calls, want) {
		t.Fatalf("result %+v calls %q", res, s.calls)
	}
}

func TestSystemdInstallOverADifferentUnitBacksItUpAndRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentws-serve.service")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &systemctl{}
	res, err := systemd.Install(context.Background(), unit(dir), s.run)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Backup != path+".bak" || len(s.calls) != 3 {
		t.Fatalf("result %+v calls %q", res, s.calls)
	}
	if old, err := os.ReadFile(res.Backup); err != nil || string(old) != "old" {
		t.Fatalf("backup %q, %v", old, err)
	}
}

func TestSystemdInstallReportsAFailedStart(t *testing.T) {
	for _, verb := range []string{"daemon-reload", "enable", "restart"} {
		s := &systemctl{fail: map[string]bool{verb: true}}
		if _, err := systemd.Install(context.Background(), unit(t.TempDir()), s.run); err == nil {
			t.Errorf("a failed %s was not reported", verb)
		}
	}
}

func TestSystemdRemoveStopsDisablesAndDeletes(t *testing.T) {
	dir := t.TempDir()
	u := unit(dir)
	if _, err := systemd.Install(context.Background(), u, (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	s := &systemctl{}
	removed, err := systemd.Remove(context.Background(), u, s.run)
	if err != nil || !removed {
		t.Fatalf("removed %v err %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agentws-serve.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit still there: %v", err)
	}
	want := [][]string{
		{"systemctl", "--user", "disable", "--now", "agentws-serve.service"},
		{"systemctl", "--user", "daemon-reload"},
	}
	if !reflect.DeepEqual(s.calls, want) {
		t.Fatalf("calls %q", s.calls)
	}
}

func TestSystemdRemoveKeepsTheUnitWhenItWillNotStop(t *testing.T) {
	dir := t.TempDir()
	u := unit(dir)
	if _, err := systemd.Install(context.Background(), u, (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	if _, err := systemd.Remove(context.Background(), u, (&systemctl{fail: map[string]bool{"disable": true}}).run); err == nil {
		t.Fatal("a failed disable was not reported")
	}
	if _, err := os.Stat(filepath.Join(dir, "agentws-serve.service")); err != nil {
		t.Fatalf("unit removed though the service may still run: %v", err)
	}
}

func TestSystemdInstallEscapesTheLogPath(t *testing.T) {
	dir := t.TempDir()
	u := unit(dir)
	u.Log = `/home/me/100%/a\b/serve.log`
	if _, err := systemd.Install(context.Background(), u, (&systemctl{}).run); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "agentws-serve.service"))
	if want := `StandardOutput=append:/home/me/100%%/a\\b/serve.log`; !strings.Contains(string(raw), want) {
		t.Fatalf("unit lacks %q:\n%s", want, raw)
	}
}

func TestSystemdInstallRefusesLineBreaksAndNULsInAnyValue(t *testing.T) {
	for name, mutate := range map[string]func(*systemd.Unit){
		"program": func(u *systemd.Unit) { u.Program = []string{"/bin/agentws", "a\nb"} },
		"env":     func(u *systemd.Unit) { u.Env = map[string]string{"X": "a\rb"} },
		"log":     func(u *systemd.Unit) { u.Log = "/tmp/a\x00b" },
	} {
		dir := t.TempDir()
		u := unit(dir)
		mutate(&u)
		s := &systemctl{}
		if _, err := systemd.Install(context.Background(), u, s.run); err == nil {
			t.Errorf("%s: a control character was written", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "agentws-serve.service")); err == nil || len(s.calls) != 0 {
			t.Errorf("%s: a unit was written or systemctl ran", name)
		}
	}
}

func TestSystemdRemoveWithoutAUnitIsANoop(t *testing.T) {
	s := &systemctl{}
	removed, err := systemd.Remove(context.Background(), unit(t.TempDir()), s.run)
	if err != nil || removed || len(s.calls) != 0 {
		t.Fatalf("removed %v err %v calls %q", removed, err, s.calls)
	}
}
