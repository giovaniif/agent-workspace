package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type evRepo struct {
	dir, base, head string
}

func newEvRepo(t *testing.T, changed string) evRepo {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "README.md"), "x\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-qb", "fix")
	write(t, filepath.Join(dir, changed), "y\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "fix")
	head := git(t, dir, "rev-parse", "HEAD")
	return evRepo{dir, base, head}
}

func realGitEnv(t *testing.T) *env {
	t.Helper()
	e, _ := testEnv(t)
	e.sys = realSys{}
	return e
}

func mustOK(t *testing.T, e *env, args ...string) map[string]any {
	t.Helper()
	got, code := runJSON(t, e, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, problemText(got))
	}
	return got
}

func TestEvidenceInitPinsMergeBaseAndHead(t *testing.T) {
	r := newEvRepo(t, "internal/daemon/x.go")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	got := mustOK(t, e, "evidence", "init", dir, "--claim", "TEST claim", "--repo", r.dir, "--main", "main")
	if got["base"] != r.base || got["head"] != r.head {
		t.Errorf("base %v head %v, want %s %s", got["base"], got["head"], r.base, r.head)
	}
	if _, err := os.Stat(filepath.Join(dir, "evidence.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceVerifyNamesEveryMissingProof(t *testing.T) {
	r := newEvRepo(t, "web/src/x.tsx")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	mustOK(t, e, "evidence", "init", dir, "--claim", "TEST", "--repo", r.dir, "--main", "main")
	got, code := runJSON(t, e, "evidence", "verify", dir)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	text := problemText(got)
	for _, want := range []string{"base repro", "head fix", "head test", "failing test", "design-check"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems lack %q:\n%s", want, text)
		}
	}
}

func fullBundle(t *testing.T, e *env, r evRepo, dir string) string {
	t.Helper()
	mustOK(t, e, "evidence", "init", dir, "--claim", "TEST", "--repo", r.dir, "--main", "main")
	art := filepath.Join(t.TempDir(), "shot.png")
	write(t, art, "image")
	git(t, r.dir, "checkout", "-q", r.base)
	mustOK(t, e, "evidence", "add", dir, "--ref", "base", "--kind", "repro", "--artifact", art, "--api-failure", "GET /api/v1/x 500")
	mustOK(t, e, "evidence", "add", dir, "--ref", "base", "--kind", "test", "--run", "exit 3")
	git(t, r.dir, "checkout", "-q", "fix")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "fix", "--viewport", "mobile", "--artifact", art, "--api-failure", "GET /api/v1/x 500")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "fix", "--viewport", "desktop", "--artifact", art)
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "test", "--run", "true")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "design", "--run", "true")
	return art
}

func TestEvidenceVerifyPassesAFullBundle(t *testing.T) {
	r := newEvRepo(t, "web/src/x.tsx")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	fullBundle(t, e, r, dir)
	mustOK(t, e, "evidence", "verify", dir)
}

func TestEvidenceVerifyCatchesTamperingMovedCheckoutAndHeadOnlyFailures(t *testing.T) {
	r := newEvRepo(t, "web/src/x.tsx")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	art := fullBundle(t, e, r, dir)
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "fix", "--viewport", "mobile", "--api-failure", "POST /api/v1/sessions 502")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "regression", "--regression", "sidebar order")
	write(t, art, "changed")
	git(t, r.dir, "checkout", "-q", "main")
	got, code := runJSON(t, e, "evidence", "verify", dir)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	text := problemText(got)
	for _, want := range []string{"hash", "checkout", "POST /api/v1/sessions 502", "sidebar order"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "GET /api/v1/x 500") {
		t.Errorf("a failure that base also has is reported:\n%s", text)
	}
}

func TestEvidenceTestThatPassesOnBaseIsNotAFailingTestFirst(t *testing.T) {
	r := newEvRepo(t, "internal/daemon/x.go")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	mustOK(t, e, "evidence", "init", dir, "--claim", "TEST", "--repo", r.dir, "--main", "main")
	git(t, r.dir, "checkout", "-q", r.base)
	mustOK(t, e, "evidence", "add", dir, "--ref", "base", "--kind", "note", "--note", "no-repro: daemon only")
	mustOK(t, e, "evidence", "add", dir, "--ref", "base", "--kind", "test", "--run", "true")
	git(t, r.dir, "checkout", "-q", "fix")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "fix", "--viewport", "tui")
	mustOK(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "test", "--run", "true")
	got, code := runJSON(t, e, "evidence", "verify", dir)
	text := problemText(got)
	if code != 1 || !strings.Contains(text, "failing test") {
		t.Errorf("exit %d:\n%s", code, text)
	}
	if strings.Contains(text, "design-check") {
		t.Errorf("a non-UI change needs a design check:\n%s", text)
	}
	if strings.Contains(text, "base repro") {
		t.Errorf("a no-repro note does not count:\n%s", text)
	}
}

func TestEvidenceAddRefusesTheWrongCheckout(t *testing.T) {
	r := newEvRepo(t, "internal/daemon/x.go")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	mustOK(t, e, "evidence", "init", dir, "--claim", "TEST", "--repo", r.dir, "--main", "main")
	got, code := runJSON(t, e, "evidence", "add", dir, "--ref", "base", "--kind", "repro")
	if code != 1 || !strings.Contains(problemText(got), "check out") {
		t.Errorf("exit %d: %s", code, problemText(got))
	}
}

func TestEvidenceAddRejectsUnknownKinds(t *testing.T) {
	r := newEvRepo(t, "a.go")
	e := realGitEnv(t)
	dir := filepath.Join(t.TempDir(), "ev")
	mustOK(t, e, "evidence", "init", dir, "--claim", "TEST", "--repo", r.dir, "--main", "main")
	_, code := runJSON(t, e, "evidence", "add", dir, "--ref", "head", "--kind", "vibes")
	if code != 2 {
		t.Errorf("exit %d", code)
	}
}
