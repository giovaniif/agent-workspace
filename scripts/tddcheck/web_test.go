package tddcheck

import (
	"strings"
	"testing"
)

const fakeVitest = `#!/bin/sh
echo "fake-vitest $1"
for t in $(find "$1" -name '*.test.ts' -o -name '*.test.tsx'); do
  line=$(sed -n 's/^expect //p' "$t")
  file=${line%% *}
  want=${line#* }
  case "$want" in
    (@*) want=$(cat "${want#@}") ;;
  esac
  grep -qF "$want" "$file" || exit 1
done
`

var webEnv = []string{"TDD_WEB_INSTALL=true", "TDD_WEB_TEST=sh fake-vitest"}

func webProject(value string) map[string]string {
	return map[string]string{
		"web/package.json":  "{}\n",
		"web/fake-vitest":   fakeVitest,
		"web/src/a.ts":      webSrc(value),
		"web/src/a.test.ts": webTest(value),
	}
}

func webSrc(value string) string {
	return "export const a = \"" + value + "\";\n"
}

func webTest(value string) string {
	return "expect src/a.ts \"" + value + "\"\n"
}

func TestOrderedWebPRPasses(t *testing.T) {
	r := newRepo(t, webProject("a"))
	r.commit("test(web): a is b", map[string]string{"web/src/a.test.ts": webTest("b")})
	r.commit("feat(web): a is b", map[string]string{"web/src/a.ts": webSrc("b")})
	code, out := r.check(webEnv...)
	if code != 0 || !strings.Contains(out, "vitest src/a.test.ts on base") {
		t.Fatalf("exit %d, want 0 after running src/a.test.ts on base\n%s", code, out)
	}
}

func TestWebTestThatPassesOnBaseFails(t *testing.T) {
	r := newRepo(t, webProject("a"))
	r.commit("test(web): another test of a", map[string]string{"web/src/b.test.tsx": webTest("a")})
	code, out := r.check(webEnv...)
	if code != 1 || !strings.Contains(out, "pass on the base branch") || !strings.Contains(out, "src/b.test.tsx") {
		t.Fatalf("exit %d, want 1 naming src/b.test.tsx, which passes on base\n%s", code, out)
	}
}

func TestCommitMixingWebTestAndCodeFails(t *testing.T) {
	r := newRepo(t, webProject("a"))
	r.commit("feat(web): a is b", map[string]string{"web/src/a.ts": webSrc("b"), "web/src/a.test.ts": webTest("b")})
	code, out := r.check(webEnv...)
	if code != 1 || !strings.Contains(out, "changes tests and production code together") {
		t.Fatalf("exit %d, want 1 naming the mixed commit\n%s", code, out)
	}
}

func TestWebTestsFailOnABaseWithoutTheWebApp(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a")})
	project := webProject("b")
	test := project["web/src/a.test.ts"]
	delete(project, "web/src/a.test.ts")
	r.commit("chore(web): the web app", project)
	r.commit("test(web): a is b", map[string]string{"web/src/a.test.ts": test})
	code, out := r.check(webEnv...)
	if code != 0 || !strings.Contains(out, "no web/package.json on base") {
		t.Fatalf("exit %d, want 0: base has no web app, so its tests cannot pass there\n%s", code, out)
	}
}

func TestWebTestHelpersAreLikePortFakes(t *testing.T) {
	r := newRepo(t, webProject("a"))
	r.commit("feat(web): a is b", map[string]string{
		"web/src/a.ts":              webSrc("b"),
		"web/src/test/fake-api.ts":  "export const fake = 1;\n",
		"web/src/test/more/deep.ts": "export const deep = 1;\n",
	})
	code, out := r.check(webEnv...)
	if code != 0 || !strings.Contains(out, "no added or changed test files") {
		t.Fatalf("exit %d, want 0: helpers under web/src/test are exempt\n%s", code, out)
	}
}

func TestWebTestdataRunsItsDirectory(t *testing.T) {
	project := webProject("a")
	project["web/src/a.test.ts"] = "expect src/a.ts @src/testdata/want.txt\n"
	project["web/src/testdata/want.txt"] = "\"a\"\n"
	r := newRepo(t, project)
	r.commit("test(web): a is b", map[string]string{"web/src/testdata/want.txt": "\"b\"\n"})
	r.commit("feat(web): a is b", map[string]string{"web/src/a.ts": webSrc("b")})
	code, out := r.check(webEnv...)
	if code != 0 || !strings.Contains(out, "vitest src on base") {
		t.Fatalf("exit %d, want 0 after running src on base\n%s", code, out)
	}
}
