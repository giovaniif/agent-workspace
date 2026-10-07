package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func configDaemon(t *testing.T, body string) (*rpc.Client, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, sock := start(t, &memStore{},
		daemon.WithHarnesses(&fakeHost{}, claude.Adapter{}, codex.Adapter{}),
		daemon.WithLauncher(0),
		daemon.WithConfig(path))
	return dial(t, sock), path
}

func setConfig(c *rpc.Client, key, value string) (rpc.ConfigValues, error) {
	var got rpc.ConfigValues
	err := c.Call(context.Background(), rpc.MethodConfigSet, rpc.ConfigSetParams{Key: key, Value: value}, &got)
	return got, err
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	got, err := filepath.Glob(path + ".agentws-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestConfigGetReadsTheSettableKeys(t *testing.T) {
	c, path := configDaemon(t, "[launcher]\nmax_parallel = 3\n[defaults.codex]\nmodel = \"gpt-6-sol\"\n[theme]\nblue = \"#000000\"\n[linear]\ntoken = \"secret\"\n")
	var got rpc.ConfigValues
	if err := c.Call(context.Background(), rpc.MethodConfigGet, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"launcher.max_parallel": "3", "defaults.codex.model": "gpt-6-sol", "theme.blue": "#000000"}
	if got.Path != path || len(got.Values) != len(want) {
		t.Fatalf("got %+v, want %v at %s", got, want, path)
	}
	for k, v := range want {
		if got.Values[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Values[k], v)
		}
	}
}

func TestConfigSetBacksUpAndKeepsTheUsersComments(t *testing.T) {
	before := "# my settings\n[launcher]\nmax_parallel = 2 # two\n\n[ui]\nmouse = false\nunknown = \"kept\"\n"
	c, path := configDaemon(t, before)
	got, err := setConfig(c, "launcher.max_parallel", "5")
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["launcher.max_parallel"] != "5" {
		t.Fatalf("reply %+v", got)
	}
	after, _ := os.ReadFile(path)
	if string(after) != "# my settings\n[launcher]\nmax_parallel = 5\n\n[ui]\nmouse = false\nunknown = \"kept\"\n" {
		t.Fatalf("file now %q", after)
	}
	saved := backups(t, path)
	if len(saved) != 1 {
		t.Fatalf("backups %v", saved)
	}
	if b, _ := os.ReadFile(saved[0]); string(b) != before {
		t.Fatalf("backup holds %q", b)
	}
}

func TestConfigSetOfTheSameValueWritesNothing(t *testing.T) {
	c, path := configDaemon(t, "[launcher]\nmax_parallel = 5\n")
	if _, err := setConfig(c, "launcher.max_parallel", "5"); err != nil {
		t.Fatal(err)
	}
	if saved := backups(t, path); len(saved) != 0 {
		t.Fatalf("an unchanged value made backups %v", saved)
	}
}

func TestConfigSetCreatesAMissingFileWithoutABackup(t *testing.T) {
	c, path := configDaemon(t, "")
	if _, err := setConfig(c, "push.away_after", "3m"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "[push]\naway_after = \"3m\"\n" {
		t.Fatalf("file %q", b)
	}
	if saved := backups(t, path); len(saved) != 0 {
		t.Fatalf("backups %v", saved)
	}
}

func TestConfigSetRefusesAnInvalidValue(t *testing.T) {
	before := "[launcher]\nmax_parallel = 2\n"
	c, path := configDaemon(t, before)
	_, err := setConfig(c, "launcher.max_parallel", "zero")
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != before {
		t.Fatalf("file changed to %q", b)
	}
}

func TestConfigSetRefusesAKeyWrittenInAFormItCannotEdit(t *testing.T) {
	before := "defaults = { claude = { model = \"opus\" } }\n"
	c, path := configDaemon(t, before)
	if _, err := setConfig(c, "defaults.claude.model", "sonnet"); err == nil {
		t.Fatal("no error")
	}
	if b, _ := os.ReadFile(path); string(b) != before {
		t.Fatalf("file changed to %q", b)
	}
	if saved := backups(t, path); len(saved) != 0 {
		t.Fatalf("backups %v", saved)
	}
}

func TestConfigSetChangesTheNewSessionDefaultsWithoutARestart(t *testing.T) {
	c, _ := configDaemon(t, "")
	if _, err := setConfig(c, "defaults.codex.model", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if _, err := setConfig(c, "launcher.max_parallel", "7"); err != nil {
		t.Fatal(err)
	}
	var got rpc.SessionOptions
	if err := c.Call(context.Background(), rpc.MethodSessionOptions, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxParallel != 7 {
		t.Fatalf("max_parallel %d", got.MaxParallel)
	}
	for _, h := range got.Harnesses {
		if h.Harness == "codex" && h.Model != "gpt-6-luna" {
			t.Fatalf("codex model %q", h.Model)
		}
	}
}

func TestConfigSetOfAnEmptyValueRemovesTheKey(t *testing.T) {
	c, path := configDaemon(t, "[defaults.claude]\nmodel = \"opus\"\neffort = \"high\"\n")
	got, err := setConfig(c, "defaults.claude.effort", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Values["defaults.claude.effort"]; ok {
		t.Fatalf("still set: %+v", got)
	}
	if b, _ := os.ReadFile(path); string(b) != "[defaults.claude]\nmodel = \"opus\"\n" {
		t.Fatalf("file %q", b)
	}
}
