package omp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

const testBin = "/opt/agent's bin/agentws"

func TestLaunchArgv(t *testing.T) {
	cases := []struct {
		name string
		req  app.LaunchRequest
		want []string
	}{
		{"everything", app.LaunchRequest{Resume: "s-1", Model: "sonnet", Effort: "high", Prompt: "--fix the build"},
			[]string{"omp", "--resume", "s-1", "--model", "sonnet", "--thinking", "high", "--", "--fix the build"}},
		{"nothing chosen", app.LaunchRequest{}, []string{"omp"}},
		{"prompt only", app.LaunchRequest{Prompt: "hi"}, []string{"omp", "--", "hi"}},
		{"effort without a model", app.LaunchRequest{Effort: "xhigh"}, []string{"omp", "--thinking", "xhigh"}},
		{"resume only", app.LaunchRequest{Resume: "s-2"}, []string{"omp", "--resume", "s-2"}},
	}
	for _, c := range cases {
		c.req.Name, c.req.Dir = "api-42", "/work/api"
		spec := Adapter{}.Launch(c.req)
		if !slices.Equal(spec.Command, c.want) || spec.Name != "api-42" || spec.Dir != "/work/api" {
			t.Errorf("%s: spec %+v, want command %q", c.name, spec, c.want)
		}
	}
	if got := (Adapter{Binary: "/bin/omp-dev"}).Launch(app.LaunchRequest{}).Command; !slices.Equal(got, []string{"/bin/omp-dev"}) {
		t.Errorf("binary override: %q", got)
	}
	if h := (Adapter{}).Harness(); h != domain.HarnessOmp {
		t.Errorf("harness %q", h)
	}
}

func TestAgentDirIsTheEnvOverrideElseHomeOmpAgent(t *testing.T) {
	env := map[string]string{"HOME": "/home/me"}
	get := func(k string) string { return env[k] }
	if got, err := AgentDir(get); err != nil || got != "/home/me/.omp/agent" {
		t.Errorf("default = %q, %v", got, err)
	}
	env["PI_CODING_AGENT_DIR"] = "/tmp/agent"
	if got, err := AgentDir(get); err != nil || got != "/tmp/agent" {
		t.Errorf("override = %q, %v", got, err)
	}
	if _, err := AgentDir(func(string) string { return "" }); err == nil {
		t.Fatal("empty HOME was accepted")
	}
}

func readGolden(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "agentws.ts.golden"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupWritesOnlyTheHookFile(t *testing.T) {
	dir := t.TempDir()
	cfg := SetupConfig{Dir: dir, Command: testBin}
	res, err := Setup(cfg)
	if err != nil || !res.Changed {
		t.Fatalf("setup = %+v, %v", res, err)
	}
	file := filepath.Join(dir, "hooks", "post", "agentws.ts")
	if res.File != file {
		t.Errorf("file = %q", res.File)
	}
	if got := read(t, file); got != readGolden(t) {
		t.Errorf("hook file:\n%s", got)
	}
	var written []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			written = append(written, strings.TrimPrefix(path, dir))
		}
		return nil
	})
	if want := []string{"/hooks/post/agentws.ts"}; !slices.Equal(written, want) {
		t.Errorf("wrote %q, want %q", written, want)
	}
}

func TestSetupTwiceDoesNotWriteAgain(t *testing.T) {
	dir := t.TempDir()
	cfg := SetupConfig{Dir: dir, Command: testBin}
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	file := HookFile(dir)
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(file, past, past); err != nil {
		t.Fatal(err)
	}
	res, err := Setup(cfg)
	if err != nil || res.Changed {
		t.Fatalf("second setup = %+v, %v", res, err)
	}
	if fi, _ := os.Stat(file); !fi.ModTime().Equal(past) {
		t.Errorf("rewritten at %v", fi.ModTime())
	}
}

func TestSetupReplacesAStaleFileOfOurs(t *testing.T) {
	dir := t.TempDir()
	file := HookFile(dir)
	writeFile(t, file, "// agentws: written by agentws setup omp; agentws setup omp --remove deletes this file.\nconst agentws = \"/old/agentws\";\n")
	res, err := Setup(SetupConfig{Dir: dir, Command: testBin})
	if err != nil || !res.Changed {
		t.Fatalf("setup = %+v, %v", res, err)
	}
	if got := read(t, file); got != readGolden(t) {
		t.Errorf("hook file:\n%s", got)
	}
}

func TestAFileOfTheUsersIsAConflictAndIsKept(t *testing.T) {
	dir := t.TempDir()
	file := HookFile(dir)
	mine := "export default function (pi) {}\n"
	writeFile(t, file, mine)
	cfg := SetupConfig{Dir: dir, Command: testBin}
	if _, err := Setup(cfg); !errors.Is(err, ErrNotOurs) {
		t.Errorf("setup err = %v", err)
	}
	if _, err := Remove(cfg); !errors.Is(err, ErrNotOurs) {
		t.Errorf("remove err = %v", err)
	}
	if ok, err := Installed(cfg); ok || !errors.Is(err, ErrNotOurs) {
		t.Errorf("installed = %v, %v", ok, err)
	}
	if got := read(t, file); got != mine {
		t.Errorf("the user's file became:\n%s", got)
	}
}

func TestRemoveDeletesOnlyOurFile(t *testing.T) {
	dir := t.TempDir()
	cfg := SetupConfig{Dir: dir, Command: testBin}
	if res, err := Remove(cfg); err != nil || res.Changed {
		t.Fatalf("remove with nothing set up = %+v, %v", res, err)
	}
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	neighbour := filepath.Join(dir, "hooks", "post", "mine.ts")
	writeFile(t, neighbour, "export default function () {}\n")
	res, err := Remove(cfg)
	if err != nil || !res.Changed {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if _, err := os.Stat(HookFile(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hook file still there: %v", err)
	}
	if got := read(t, neighbour); got != "export default function () {}\n" {
		t.Errorf("neighbour = %q", got)
	}
}

func TestInstalledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := SetupConfig{Dir: dir, Command: testBin}
	if ok, err := Installed(cfg); ok || err != nil {
		t.Fatalf("installed on a fresh dir = %v, %v", ok, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("Installed wrote %v", entries)
	}
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	if ok, err := Installed(cfg); !ok || err != nil {
		t.Fatalf("installed after setup = %v, %v", ok, err)
	}
	if ok, _ := Installed(SetupConfig{Dir: dir, Command: "/moved/agentws"}); ok {
		t.Fatal("a moved binary still counts as installed")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const hookHarness = `
const calls = [];
const pi = {
	on: (name, handler) => calls.push([name, handler]),
	getThinkingLevel: () => "high",
};
const mod = await import(process.argv[2]);
mod.default(pi);
const ctx = { cwd: "/work/api", model: { provider: "anthropic", id: "sonnet" }, sessionManager: { getSessionId: () => "s-1" } };
const run = (name, event) => calls.find(c => c[0] === name)[1](event, ctx);
const fromToolCall = run("tool_call", { toolName: "bash", input: { command: "ls" } });
console.log("tool_call returned " + typeof fromToolCall);
await run("tool_approval_resolved", { approved: true });
await run("tool_approval_resolved", { approved: false, reason: "no" });
await run("session_stop", { last_assistant_message: { content: [{ type: "text", text: "all done" }] } });
await new Promise(r => setTimeout(r, 300));
console.log(calls.map(c => c[0]).join(","));
`

// why: proves the generated file loads and sends what the daemon reads; it
// needs bun, which omp itself runs on.
func TestHookFileSendsTheEventsUnderBun(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is not on PATH")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	fake := filepath.Join(dir, "agentws")
	writeFile(t, fake, "#!/bin/sh\nprintf '%s %s ' \"$3\" \"$5\" >> '"+log+"'\ncat >> '"+log+"'\necho >> '"+log+"'\n")
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(SetupConfig{Dir: dir, Command: fake}); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "run.mjs")
	writeFile(t, harness, hookHarness)
	out, err := exec.Command(bun, harness, HookFile(dir)).CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if want := "tool_call returned undefined\nagent_start,session_shutdown,session_start,session_stop,tool_approval_requested,tool_approval_resolved,tool_call,tool_result\n"; string(out) != want {
		t.Errorf("bun printed %q, want %q", out, want)
	}
	lines := strings.Split(strings.TrimSpace(read(t, log)), "\n")
	slices.Sort(lines)
	want := []string{
		`omp session_stop {"session_id":"s-1","cwd":"/work/api","model":"anthropic/sonnet","effort":"high","last_assistant_message":"all done"}`,
		`omp tool_approval_resolved {"session_id":"s-1","cwd":"/work/api","model":"anthropic/sonnet","effort":"high","message":"no"}`,
		`omp tool_call {"session_id":"s-1","cwd":"/work/api","model":"anthropic/sonnet","effort":"high","tool_name":"bash","tool_input":{"command":"ls"}}`,
	}
	if !slices.Equal(lines, want) {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestParseCatalogUsesSelectors(t *testing.T) {
	got, err := ParseCatalog([]byte(`{"models":[
		{"selector":"openai-codex/gpt-5.5","id":"gpt-5.5"},
		{"selector":"","id":"skip"},
		{"id":"no-selector"},
		{"selector":"opencode-go/deepseek"},
		{"selector":"openai-codex/gpt-5.5"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"openai-codex/gpt-5.5", "opencode-go/deepseek"}
	if !slices.Equal(got, want) {
		t.Fatalf("catalog %q, want %q", got, want)
	}
}

func TestFetchCatalogReadsOmpModelsJSON(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "omp")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"models\":[{\"selector\":\"prov/b\"},{\"selector\":\"prov/a\"}]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FetchCatalog(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"prov/a", "prov/b"}
	if !slices.Equal(got, want) {
		t.Fatalf("catalog %q, want %q", got, want)
	}
}

func TestLoadCachedReplacesAFreshFileWhenTheCallerWaits(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "omp-models.json")
	if err := os.WriteFile(cache, []byte(`["amazon-bedrock/old"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "omp")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"models\":[{\"selector\":\"cursor/new\"}]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got := LoadCached(context.Background(), cache, bin, 24*time.Hour, true)
	want := []string{"cursor/new"}
	if !slices.Equal(got, want) {
		t.Fatalf("catalog %q, want %q", got, want)
	}
	body, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `["cursor/new"]` {
		t.Fatalf("cache %s", body)
	}
}

func TestLoadCachedUsesAFreshFileWithoutRunningOmp(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "omp-models.json")
	if err := os.WriteFile(cache, []byte(`["prov/a","prov/b"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(dir, "ran")
	bin := filepath.Join(dir, "omp")
	script := "#!/bin/sh\ntouch \"" + ran + "\"\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got := LoadCached(context.Background(), cache, bin, time.Hour, false)
	if !slices.Equal(got, []string{"prov/a", "prov/b"}) {
		t.Fatalf("catalog %q", got)
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("fresh cache still ran omp")
	}
}

func TestLoadCachedRefreshesAStaleFileWhenBlocked(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "omp-models.json")
	if err := os.WriteFile(cache, []byte(`["old/model"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(cache, past, past); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "omp")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"models\":[{\"selector\":\"prov/b\"},{\"selector\":\"prov/a\"}]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got := LoadCached(context.Background(), cache, bin, time.Hour, true)
	want := []string{"prov/a", "prov/b"}
	if !slices.Equal(got, want) {
		t.Fatalf("catalog %q, want %q", got, want)
	}
	body, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `["prov/a","prov/b"]` {
		t.Fatalf("cache %s", body)
	}
}
