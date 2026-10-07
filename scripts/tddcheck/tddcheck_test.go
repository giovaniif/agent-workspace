package tddcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repo struct {
	t    *testing.T
	dir  string
	base string
}

func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	r.git("config", "user.email", "t@example.com")
	r.git("config", "user.name", "t")
	r.write(map[string]string{"go.mod": "module example.com/x\n\ngo 1.22\n"})
	r.commit("chore: base", files)
	r.base = r.git("rev-parse", "HEAD")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(files map[string]string) {
	r.t.Helper()
	for name, body := range files {
		path := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *repo) commit(msg string, files map[string]string) {
	r.t.Helper()
	r.write(files)
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

func (r *repo) move(from, to string) {
	r.t.Helper()
	r.git("mv", from, to)
}

func (r *repo) check(env ...string) (int, string) {
	r.t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "tdd-check"))
	if err != nil {
		r.t.Fatal(err)
	}
	head := r.git("rev-parse", "HEAD")
	cmd := exec.Command("bash", script, r.base, head)
	cmd.Dir = r.dir
	cmd.Env = append(append(os.Environ(), "TDD_PR_TITLE=feat: x"), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return code, string(out)
}

func (r *repo) checkRefs(base, head string) (int, string) {
	r.t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "tdd-check"))
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command("bash", script, base, head)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "TDD_PR_TITLE=feat: x")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return code, string(out)
}

func (r *repo) assertRestored(branch, sha string) {
	r.t.Helper()
	if got := r.git("rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		r.t.Fatalf("HEAD is %q after the check, want branch %q", got, branch)
	}
	if got := r.git("rev-parse", "HEAD"); got != sha {
		r.t.Fatalf("HEAD at %s after the check, want %s", got, sha)
	}
	if st := r.git("status", "--porcelain"); st != "" {
		r.t.Fatalf("worktree not clean after the check:\n%s", st)
	}
}

func TestSymbolicHeadResolvesBeforeCheckout(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("test: A returns b", map[string]string{"p/a_test.go": test("TestA", "b")})
	r.commit("feat: A returns b", map[string]string{"p/a.go": src("b")})
	branch := r.git("rev-parse", "--abbrev-ref", "HEAD")
	sha := r.git("rev-parse", "HEAD")
	if code, out := r.checkRefs(r.base, "HEAD"); code != 0 || !strings.Contains(out, "every new or changed test fails on base") {
		t.Fatalf("exit %d, want 0 with the base run\n%s", code, out)
	}
	r.assertRestored(branch, sha)
}

func TestFailingCheckRestoresBranchAndIndex(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("test: A again", map[string]string{"p/b_test.go": "package p\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { TestA(t) }\n"})
	branch := r.git("rev-parse", "--abbrev-ref", "HEAD")
	sha := r.git("rev-parse", "HEAD")
	if code, out := r.checkRefs(r.base, sha); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	r.assertRestored(branch, sha)
}

func src(ret string) string {
	return "package p\n\nfunc A() string { return \"" + ret + "\" }\n"
}

func test(name, want string) string {
	return "package p\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {\n\tif A() != \"" + want + "\" {\n\t\tt.Fatal(A())\n\t}\n}\n"
}

func TestOrderedPRPasses(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("test: A returns b", map[string]string{"p/a_test.go": test("TestA", "b")})
	r.commit("feat: A returns b", map[string]string{"p/a.go": src("b")})
	if code, out := r.check(); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
}

func TestCommitMixingTestAndCodeFails(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("feat: A returns b", map[string]string{"p/a.go": src("b"), "p/a_test.go": test("TestA", "b")})
	code, out := r.check()
	if code != 1 || !strings.Contains(out, "changes tests and production code together") {
		t.Fatalf("exit %d, want 1 naming the mixed commit\n%s", code, out)
	}
}

func TestMovedTestThatPassesOnBaseFails(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.move("p/a_test.go", "p/b_test.go")
	r.commit("test: move and extend the A test", map[string]string{
		"p/b_test.go": test("TestA", "a") + "\nfunc TestAgain(t *testing.T) { TestA(t) }\n",
	})
	code, out := r.check()
	if code != 1 || !strings.Contains(out, "pass on the base branch") {
		t.Fatalf("exit %d, want 1 because the moved test passes on base\n%s", code, out)
	}
}

func TestMovedTestThatFailsOnBasePasses(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.move("p/a_test.go", "p/b_test.go")
	r.commit("test: move the A test and expect b", map[string]string{"p/b_test.go": test("TestA", "b")})
	r.commit("feat: A returns b", map[string]string{"p/a.go": src("b")})
	code, out := r.check()
	if code != 0 || !strings.Contains(out, "go test ./p on base") {
		t.Fatalf("exit %d, want 0 after running ./p on base\n%s", code, out)
	}
}

func TestWhitespaceInsideAStringCounts(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a b"), "p/a_test.go": test("TestA", "a b")})
	r.commit("feat: A drops the space", map[string]string{"p/a.go": src("ab"), "p/a_test.go": test("TestA", "ab")})
	code, out := r.check()
	if code != 1 || !strings.Contains(out, "changes tests and production code together") {
		t.Fatalf("exit %d, want 1: the test's expected value changed\n%s", code, out)
	}
}

func TestIndentationOnlyTestChangeIsIgnored(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("feat: A returns b", map[string]string{
		"p/a.go":      src("b"),
		"p/a_test.go": strings.ReplaceAll(test("TestA", "a"), "\t", "    "),
	})
	if code, out := r.check(); code != 0 {
		t.Fatalf("exit %d, want 0: the test change is whitespace only\n%s", code, out)
	}
}

func TestBlockCommentOnlyTestChangeIsIgnored(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.commit("feat: A returns b", map[string]string{
		"p/a.go":      src("b"),
		"p/a_test.go": strings.Replace(test("TestA", "a"), "func TestA", "/* why: A is the only export. */\nfunc TestA", 1),
	})
	if code, out := r.check(); code != 0 {
		t.Fatalf("exit %d, want 0: the test change is a comment only\n%s", code, out)
	}
}

func TestRewrittenMoveThatPassesOnBaseFails(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a"), "p/a_test.go": test("TestA", "a")})
	r.git("rm", "-q", "p/a_test.go")
	var more strings.Builder
	for _, n := range []string{"One", "Two", "Three", "Four", "Five", "Six"} {
		more.WriteString("\nfunc Test" + n + "(t *testing.T) {\n\tif len(A()) != 1 {\n\t\tt.Fatal(A())\n\t}\n}\n")
	}
	r.commit("test: rewrite the A tests in a new file", map[string]string{"p/b_test.go": test("TestA", "a") + more.String()})
	code, out := r.check()
	if code != 1 || !strings.Contains(out, "pass on the base branch") {
		t.Fatalf("exit %d, want 1 because the rewritten tests pass on base\n%s", code, out)
	}
}
