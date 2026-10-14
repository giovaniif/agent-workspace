package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/rpc"
)

const globalMates = `
[launcher]
max_parallel = 2

[[mate]]
name = "reviewer"
prompt = "Review code."
agent = "claude"
repos = ["api"]
owner = "U012ABCDEF"

[[mate]]
name = "builder"
prompt = "Build things."
agent = "codex"
effort = "high"
repos = ["/shop/web"]
enabled = false
`

func mateDaemon(t *testing.T, body string) (*rpc.Client, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, sock := start(t, &memStore{}, daemon.WithWorkspaces(shop, shopGit), daemon.WithConfig(path))
	c := dial(t, sock)
	if _, err := c.WorkspaceAdd(context.Background(), "/shop"); err != nil {
		t.Fatal(err)
	}
	return c, path
}

func listMates(t *testing.T, c *rpc.Client) rpc.MateList {
	t.Helper()
	var got rpc.MateList
	if err := c.Call(context.Background(), rpc.MethodMateList, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func callCode(err error) string {
	var rerr *rpc.Error
	if errors.As(err, &rerr) {
		return rerr.Code
	}
	return "other: " + err.Error()
}

func TestMateListReadsTheConfigAndKeepsTheOthersWhenOneIsInvalid(t *testing.T) {
	body := globalMates + `
[[mate]]
name = "broken"
prompt = "x"
agent = "gemini"

[[mate]]
name = "lost"
prompt = "x"
agent = "claude"
repos = ["billing"]

[[mate]]
name = "reviewer"
prompt = "A second reviewer."
agent = "claude"
`
	c, path := mateDaemon(t, body)
	got := listMates(t, c)
	if len(got.Mates) != 2 || got.Mates[0].Name != "reviewer" || got.Mates[1].Name != "builder" {
		t.Fatalf("mates %+v", got.Mates)
	}
	r, b := got.Mates[0], got.Mates[1]
	if !r.Enabled || r.Owner != "U012ABCDEF" || r.Agent != "claude" || len(r.Repos) != 1 || r.Prompt != "Review code." {
		t.Errorf("reviewer %+v", r)
	}
	if b.Enabled || b.Effort != "high" {
		t.Errorf("builder %+v", b)
	}
	if len(got.Problems) != 3 {
		t.Fatalf("problems %v", got.Problems)
	}
	for i, want := range []string{"broken", "billing", "twice"} {
		if !strings.Contains(got.Problems[i], want) || !strings.Contains(got.Problems[i], path) {
			t.Errorf("problem %d = %q, want the path and %q", i, got.Problems[i], want)
		}
	}
}

func TestMateListReportsAnUnknownKey(t *testing.T) {
	c, _ := mateDaemon(t, "[[mate]]\nname = \"a\"\nprompt = \"x\"\nagent = \"claude\"\ntoken = \"xoxb-1\"\n")
	got := listMates(t, c)
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "unknown mate key") || !strings.Contains(got.Problems[0], "token") {
		t.Errorf("problems %v", got.Problems)
	}
	if len(got.Mates) != 1 {
		t.Errorf("mates %+v", got.Mates)
	}
}

func TestMateListWithoutMatesOrConfigIsEmpty(t *testing.T) {
	c, _ := mateDaemon(t, "")
	if got := listMates(t, c); len(got.Mates) != 0 || len(got.Problems) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestMateListReportsAConfigThatDoesNotParse(t *testing.T) {
	c, _ := mateDaemon(t, "[[mate\nname = 1")
	got := listMates(t, c)
	if len(got.Mates) != 0 || len(got.Problems) != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestMateMethodsNeedAConfig(t *testing.T) {
	_, sock := start(t, &memStore{})
	c := dial(t, sock)
	for _, m := range []string{rpc.MethodMateList, rpc.MethodMateShow, rpc.MethodMateSet, rpc.MethodMateRemove} {
		if code := callCode(c.Call(context.Background(), m, struct{}{}, nil)); code != rpc.CodeUnavailable {
			t.Errorf("%s: code %s, want unavailable", m, code)
		}
	}
}

func TestMateShowFindsAMateOrIsNotFound(t *testing.T) {
	c, _ := mateDaemon(t, globalMates)
	var m rpc.Mate
	if err := c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{Name: "builder"}, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "builder" || m.Agent != "codex" {
		t.Errorf("got %+v", m)
	}
	err := c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{Name: "ghost"}, &m)
	if callCode(err) != rpc.CodeNotFound {
		t.Errorf("got %v, want not_found", err)
	}
	if callCode(c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{}, &m)) != rpc.CodeBadRequest {
		t.Error("show without a name should be bad_request")
	}
}

func TestMateSetAddsAMateWithABackupAndKeepsTheRest(t *testing.T) {
	c, path := mateDaemon(t, globalMates)
	add := rpc.Mate{Name: "scout", Prompt: "Look around.\nBe brief.", Agent: "claude", Model: "opus", Repos: []string{"api", "web"}, Enabled: true}
	var got rpc.Mate
	if err := c.Call(context.Background(), rpc.MethodMateSet, rpc.MateSetParams{Mate: add}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "scout" {
		t.Errorf("got %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "max_parallel = 2") || !strings.Contains(string(data), `name = "reviewer"`) || !strings.Contains(string(data), `prompt = "Look around.\nBe brief."`) {
		t.Errorf("config:\n%s", data)
	}
	if len(backups(t, path)) != 1 {
		t.Errorf("backups %v", backups(t, path))
	}
	list := listMates(t, c)
	if len(list.Mates) != 3 || list.Mates[2].Name != "scout" || list.Mates[2].Prompt != "Look around.\nBe brief." || len(list.Problems) != 0 {
		t.Errorf("list %+v", list)
	}
}

func TestMateSetReplacesAMateOfTheSameName(t *testing.T) {
	c, path := mateDaemon(t, globalMates)
	edit := rpc.Mate{Name: "reviewer", Prompt: "Review harder.", Agent: "codex", Owner: "U012ABCDEF", Enabled: true}
	if err := c.Call(context.Background(), rpc.MethodMateSet, rpc.MateSetParams{Mate: edit}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "[[mate]]") != 2 || strings.Contains(string(data), "Review code.") {
		t.Errorf("config:\n%s", data)
	}
	list := listMates(t, c)
	if list.Mates[0].Prompt != "Review harder." || list.Mates[0].Agent != "codex" || len(list.Mates[0].Repos) != 0 {
		t.Errorf("list %+v", list.Mates)
	}
}

func TestMateSetRefusesAnInvalidMateWithoutWriting(t *testing.T) {
	c, path := mateDaemon(t, globalMates)
	for name, m := range map[string]rpc.Mate{
		"agent":  {Name: "x", Prompt: "y", Agent: "gemini"},
		"repo":   {Name: "x", Prompt: "y", Agent: "claude", Repos: []string{"billing"}},
		"owner":  {Name: "x", Prompt: "y", Agent: "claude", Owner: "me"},
		"prompt": {Name: "x", Agent: "claude"},
	} {
		err := c.Call(context.Background(), rpc.MethodMateSet, rpc.MateSetParams{Mate: m}, nil)
		if callCode(err) != rpc.CodeBadRequest {
			t.Errorf("%s: got %v, want bad_request", name, err)
		}
	}
	if len(backups(t, path)) != 0 {
		t.Error("an invalid set wrote the file")
	}
}

func TestMateSetLeavesAConfigThatDoesNotParseAlone(t *testing.T) {
	c, path := mateDaemon(t, "[[mate\nname = 1")
	err := c.Call(context.Background(), rpc.MethodMateSet, rpc.MateSetParams{Mate: rpc.Mate{Name: "x", Prompt: "y", Agent: "claude", Enabled: true}}, nil)
	if callCode(err) != rpc.CodeFailed {
		t.Errorf("got %v, want failed", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "[[mate\nname = 1" {
		t.Errorf("config changed: %q", data)
	}
}

func TestMateRemoveDeletesTheBlockWithABackup(t *testing.T) {
	c, path := mateDaemon(t, globalMates)
	var gone rpc.Mate
	if err := c.Call(context.Background(), rpc.MethodMateRemove, rpc.MateRemoveParams{Name: "reviewer"}, &gone); err != nil {
		t.Fatal(err)
	}
	if gone.Name != "reviewer" {
		t.Errorf("got %+v", gone)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "reviewer") || !strings.Contains(string(data), "builder") || !strings.Contains(string(data), "max_parallel = 2") {
		t.Errorf("config:\n%s", data)
	}
	if len(backups(t, path)) != 1 {
		t.Errorf("backups %v", backups(t, path))
	}
	if list := listMates(t, c); len(list.Mates) != 1 || list.Mates[0].Name != "builder" {
		t.Errorf("list %+v", list)
	}
}

func TestMateRemoveOfAnUnknownMateIsNotFoundAndWritesNothing(t *testing.T) {
	c, path := mateDaemon(t, globalMates)
	err := c.Call(context.Background(), rpc.MethodMateRemove, rpc.MateRemoveParams{Name: "ghost"}, nil)
	if callCode(err) != rpc.CodeNotFound {
		t.Errorf("got %v, want not_found", err)
	}
	if len(backups(t, path)) != 0 {
		t.Error("a missing mate wrote the file")
	}
	if callCode(c.Call(context.Background(), rpc.MethodMateRemove, rpc.MateRemoveParams{}, nil)) != rpc.CodeBadRequest {
		t.Error("remove without a name should be bad_request")
	}
}
