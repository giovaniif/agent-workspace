package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

var binDir string

const waitWithin = 15 * time.Second

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "crew-e2e")
	if err != nil {
		panic(err)
	}
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/giovaniif/crew/internal/version.Version=v0.0.0-e2e -X github.com/giovaniif/crew/internal/version.Commit=e2ecommit",
		"-o", filepath.Join(dir, "crew"), "../../cmd/crew")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		os.Exit(1)
	}
	binDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestScripts(t *testing.T) {
	fakes, err := filepath.Abs("testdata/bin")
	if err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			return setup(env, fakes)
		},
		Condition: condition,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"eventually": eventually,
			"capture":    capture,
			"repo":       repo,
		},
	})
}

func setup(env *testscript.Env, fakes string) error {
	sep := string(os.PathListSeparator)
	env.Setenv("CREW_E2E_FAKES", fakes)
	env.Setenv("CREW_E2E_BARE_PATH", barePath())
	env.Setenv("PATH", binDir+sep+fakes+sep+env.Getenv("PATH"))
	tmp, err := os.MkdirTemp("", "aws")
	if err != nil {
		return err
	}
	home, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		return err
	}
	work, err := filepath.EvalSymlinks(env.WorkDir)
	if err != nil {
		return err
	}
	socket := fmt.Sprintf("crew-e2e-%d-%d", os.Getpid(), time.Now().UnixNano())
	for k, v := range map[string]string{
		"CREW_HOME":            home,
		"CREW_NO_UPDATE_CHECK": "1",
		"CREW_E2E":             work,
		"CREW_TMUX_SOCKET":     socket,
		"CREW_TEST_CLOCK":      "+5h",
		"CREW_TEST_PR_POLL":    "100ms",
		"GIT_CONFIG_GLOBAL":    "/dev/null",
		"GIT_CONFIG_SYSTEM":    "/dev/null",
		"GIT_AUTHOR_NAME":      "t",
		"GIT_AUTHOR_EMAIL":     "t@example.com",
		"GIT_COMMITTER_NAME":   "t",
		"GIT_COMMITTER_EMAIL":  "t@example.com",
		"GIT_TERMINAL_PROMPT":  "0",
	} {
		env.Setenv(k, v)
	}
	if err := os.MkdirAll(filepath.Join(work, "gates"), 0o755); err != nil {
		return err
	}
	env.Defer(func() {
		stop := exec.Command(filepath.Join(binDir, "crew"), "daemon", "stop")
		stop.Env = append(os.Environ(), "CREW_HOME="+home)
		_ = stop.Run()
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), socket))
		_ = os.RemoveAll(home)
	})
	return nil
}

func eventually(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("usage: [!] eventually <regexp> <command> [args...]")
	}
	re, err := regexp.Compile(`(?m)` + args[0])
	ts.Check(err)
	deadline := time.Now().Add(waitWithin)
	for {
		runErr := ts.Exec(args[1], args[2:]...)
		out := ts.ReadFile("stdout")
		if runErr == nil && re.MatchString(out) != neg {
			return
		}
		if time.Now().After(deadline) {
			want := "no match"
			if neg {
				want = "still a match"
			}
			ts.Fatalf("%s for %q after %v; last stdout:\n%s\nstderr:\n%s\nerr: %v", want, args[0], waitWithin, out, ts.ReadFile("stderr"), runErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func capture(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: capture <var> <regexp>")
	}
	re, err := regexp.Compile(`(?m)` + args[1])
	ts.Check(err)
	m := re.FindStringSubmatch(ts.ReadFile("stdout"))
	if len(m) < 2 {
		ts.Fatalf("no match for %q in stdout", args[1])
	}
	ts.Setenv(args[0], m[1])
}

func repo(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: repo <name>")
	}
	name := args[0]
	work := ts.Getenv("CREW_E2E")
	seed := filepath.Join(work, "seed-"+name)
	bare := filepath.Join(work, "origin-"+name+".git")
	dir := filepath.Join(work, name)
	ts.Check(os.MkdirAll(seed, 0o755))
	ts.Check(os.WriteFile(filepath.Join(seed, "README.md"), []byte(name+"\n"), 0o644))
	for _, c := range [][]string{
		{"-C", seed, "init", "-q", "-b", "main"},
		{"-C", seed, "add", "."},
		{"-C", seed, "commit", "-q", "-m", "init"},
		{"clone", "-q", "--bare", seed, bare},
		{"clone", "-q", bare, dir},
		{"-C", dir, "remote", "set-url", "origin", "https://github.com/o/" + name + ".git"},
	} {
		if err := ts.Exec("git", c...); err != nil {
			ts.Fatalf("git %s: %v\n%s", strings.Join(c, " "), err, ts.ReadFile("stderr"))
		}
	}
}

func barePath() string {
	dirs := []string{binDir}
	for _, tool := range []string{"git", "tmux"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	dirs = append(dirs, "/usr/bin", "/bin")
	return strings.Join(dirs, string(os.PathListSeparator))
}

func condition(cond string) (bool, error) {
	if cond != "barepath" {
		return false, fmt.Errorf("unknown condition %q", cond)
	}
	for _, dir := range filepath.SplitList(barePath()) {
		for _, harness := range []string{"claude", "codex"} {
			if _, err := os.Stat(filepath.Join(dir, harness)); err == nil {
				return false, nil
			}
		}
	}
	return true, nil
}
