//go:build integration

package setup_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/setup"
	"github.com/giovaniif/crew/internal/app"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func realTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func project(t *testing.T, recipe string) (main, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := realTemp(t)
	main = filepath.Join(tmp, "api")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, main, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(main, "package-lock.json"), "{}")
	writeFile(t, filepath.Join(main, ".gitignore"), "node_modules\n.env.example\n.cache\n")
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-q", "-m", "init")
	writeFile(t, filepath.Join(main, ".env.example"), "TOKEN=changeme\n")
	writeFile(t, filepath.Join(main, ".cache", "warm"), "warm")
	writeFile(t, filepath.Join(main, "node_modules", "left-pad", "index.js"), "module.exports = 1")
	writeFile(t, filepath.Join(main, ".agentws.toml"), recipe)

	worktree = filepath.Join(tmp, "api-feature")
	gitIn(t, main, "worktree", "add", "-q", "-b", "feature", worktree)
	return main, worktree
}

func service(out *bytes.Buffer) app.WorktreeSetup {
	return app.WorktreeSetup{
		Recipes: setup.Recipes{},
		FS:      setup.FS{},
		Runner:  setup.Shell{Out: out},
		Git:     git.Worktrees{},
		Now:     time.Now,
	}
}

func TestRecipeCopyLinkAndClone(t *testing.T) {
	main, worktree := project(t, `
[setup]
copy = [".env.example"]
link = [".cache"]
deps = "clone"
run = ["echo ready > ran.txt"]
`)
	var out bytes.Buffer
	report, err := service(&out).Run(context.Background(), worktree)
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}

	if got, _ := os.ReadFile(filepath.Join(worktree, ".env.example")); string(got) != "TOKEN=changeme\n" {
		t.Errorf(".env.example = %q, want a copy of the main checkout's", got)
	}
	if info, _ := os.Lstat(filepath.Join(worktree, ".env.example")); info.Mode()&os.ModeSymlink != 0 {
		t.Error(".env.example is a symlink, want a real copy")
	}
	target, err := os.Readlink(filepath.Join(worktree, ".cache"))
	if err != nil || target != filepath.Join(main, ".cache") {
		t.Errorf(".cache -> %q (%v), want a link to the main checkout's", target, err)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "node_modules", "left-pad", "index.js"))
	if err != nil || string(got) != "module.exports = 1" {
		t.Errorf("node_modules file = %q, %v, want the cloned content", got, err)
	}
	if info, _ := os.Lstat(filepath.Join(worktree, "node_modules")); info.Mode()&os.ModeSymlink != 0 {
		t.Error("node_modules is a symlink, want a clone")
	}
	if got, _ := os.ReadFile(filepath.Join(worktree, "ran.txt")); strings.TrimSpace(string(got)) != "ready" {
		t.Errorf("ran.txt = %q, want the run command to have executed in the worktree", got)
	}

	writeFile(t, filepath.Join(worktree, "node_modules", "left-pad", "index.js"), "changed")
	if got, _ := os.ReadFile(filepath.Join(main, "node_modules", "left-pad", "index.js")); string(got) != "module.exports = 1" {
		t.Errorf("main's node_modules changed to %q", got)
	}
	if report.Duration <= 0 {
		t.Errorf("Duration = %v, want it measured", report.Duration)
	}
}

func TestRecipeLockfileMismatchRunsInstallAndLogsWhy(t *testing.T) {
	_, worktree := project(t, "[setup]\ndeps = \"clone\"\n")
	writeFile(t, filepath.Join(worktree, "package-lock.json"), `{"changed":true}`)

	bin := t.TempDir()
	marker := filepath.Join(realTemp(t), "npm-args")
	writeFile(t, filepath.Join(bin, "npm"), "#!/bin/sh\necho \"$@\" > "+marker+"\n")
	if err := os.Chmod(filepath.Join(bin, "npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	report, err := service(&out).Run(context.Background(), worktree)
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if got, _ := os.ReadFile(marker); strings.TrimSpace(string(got)) != "ci" {
		t.Errorf("npm was run with %q, want ci", got)
	}
	if _, err := os.Lstat(filepath.Join(worktree, "node_modules")); err == nil {
		t.Error("node_modules was cloned despite the lockfile change")
	}
	want := "deps install: package-lock.json differs from the main checkout (npm ci)"
	if len(report.Lines) != 1 || report.Lines[0] != want {
		t.Errorf("lines = %q, want %q", report.Lines, want)
	}
}

func TestRecipeSetupIsRepeatable(t *testing.T) {
	_, worktree := project(t, "[setup]\ncopy = [\".env.example\"]\ndeps = \"clone\"\n")
	var out bytes.Buffer
	if _, err := service(&out).Run(context.Background(), worktree); err != nil {
		t.Fatal(err)
	}
	report, err := service(&out).Run(context.Background(), worktree)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	want := []string{
		"skip copy .env.example: already in the worktree",
		"deps skip: node_modules already in the worktree",
	}
	if strings.Join(report.Lines, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", report.Lines, want)
	}
}

func TestRecipeMainCheckoutOfLinkedWorktree(t *testing.T) {
	main, worktree := project(t, "")
	got, err := git.Worktrees{}.MainCheckout(context.Background(), worktree)
	if err != nil || got != main {
		t.Errorf("MainCheckout = %q, %v, want %q", got, err, main)
	}
	if _, err := (git.Worktrees{}).MainCheckout(context.Background(), t.TempDir()); err == nil {
		t.Error("want an error outside a repo")
	}
}
