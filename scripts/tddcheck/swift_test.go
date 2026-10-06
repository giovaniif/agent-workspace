package tddcheck

import (
	"strings"
	"testing"
)

const fakeSwift = `#!/bin/sh
echo "fake-swift $1"
t="Tests/$(echo "$1" | tr . /).swift"
[ -f "$t" ] || exit 1
line=$(sed -n 's/^expect //p' "$t")
file=${line%% *}
want=${line#* }
grep -qF "$want" "$file" || exit 1
`

var swiftEnv = []string{"TDD_SWIFT_TEST=sh fake-swift"}

func swiftProject(value string) map[string]string {
	return map[string]string{
		"macos/Package.swift":               "// swift-tools-version: 6.0\n",
		"macos/fake-swift":                  fakeSwift,
		"macos/Sources/Kit/A.swift":         swiftSrc(value),
		"macos/Tests/KitTests/ATests.swift": swiftTest(value),
	}
}

func swiftSrc(value string) string {
	return "let a = \"" + value + "\"\n"
}

func swiftTest(value string) string {
	return "expect Sources/Kit/A.swift \"" + value + "\"\n"
}

func TestOrderedSwiftPRPasses(t *testing.T) {
	r := newRepo(t, swiftProject("a"))
	r.commit("test(macos): a is b", map[string]string{"macos/Tests/KitTests/ATests.swift": swiftTest("b")})
	r.commit("feat(macos): a is b", map[string]string{"macos/Sources/Kit/A.swift": swiftSrc("b")})
	code, out := r.check(swiftEnv...)
	if code != 0 || !strings.Contains(out, "swift test KitTests.ATests on base") {
		t.Fatalf("exit %d, want 0 after running KitTests.ATests on base\n%s", code, out)
	}
}

func TestSwiftTestThatPassesOnBaseFails(t *testing.T) {
	r := newRepo(t, swiftProject("a"))
	r.commit("test(macos): another test of a", map[string]string{"macos/Tests/KitTests/BTests.swift": swiftTest("a")})
	code, out := r.check(swiftEnv...)
	if code != 1 || !strings.Contains(out, "pass on the base branch") || !strings.Contains(out, "KitTests.BTests") {
		t.Fatalf("exit %d, want 1 naming KitTests.BTests, which passes on base\n%s", code, out)
	}
}

func TestCommitMixingSwiftTestAndCodeFails(t *testing.T) {
	r := newRepo(t, swiftProject("a"))
	r.commit("feat(macos): a is b", map[string]string{"macos/Sources/Kit/A.swift": swiftSrc("b"), "macos/Tests/KitTests/ATests.swift": swiftTest("b")})
	code, out := r.check(swiftEnv...)
	if code != 1 || !strings.Contains(out, "changes tests and production code together") {
		t.Fatalf("exit %d, want 1 naming the mixed commit\n%s", code, out)
	}
}

func TestSwiftTestsFailOnABaseWithoutThePackage(t *testing.T) {
	r := newRepo(t, map[string]string{"p/a.go": src("a")})
	r.commit("test(macos): a is b", map[string]string{"macos/Tests/KitTests/ATests.swift": swiftTest("b")})
	r.commit("feat(macos): the package", map[string]string{
		"macos/Package.swift":       "// swift-tools-version: 6.0\n",
		"macos/Sources/Kit/A.swift": swiftSrc("b"),
	})
	code, out := r.check(swiftEnv...)
	if code != 0 || !strings.Contains(out, "no macos/Package.swift on base") {
		t.Fatalf("exit %d, want 0: base has no Swift package, so its tests cannot pass there\n%s", code, out)
	}
}
