package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const designDiff = `diff --git a/internal/tui/view.go b/internal/tui/view.go
--- a/internal/tui/view.go
+++ b/internal/tui/view.go
@@ -1,3 +1,6 @@
 package tui
+var a = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
+var b = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Red))
+var c = lipgloss.Color("12")
-var d = lipgloss.Color("#00ff00")
diff --git a/internal/tui/theme.go b/internal/tui/theme.go
--- a/internal/tui/theme.go
+++ b/internal/tui/theme.go
@@ -1,1 +1,2 @@
+	Red: "#d20f39",
diff --git a/internal/tui/view_test.go b/internal/tui/view_test.go
--- a/internal/tui/view_test.go
+++ b/internal/tui/view_test.go
@@ -1,1 +1,2 @@
+var x = lipgloss.Color("#123456")
diff --git a/web/src/theme.css b/web/src/theme.css
--- a/web/src/theme.css
+++ b/web/src/theme.css
@@ -1,2 +1,5 @@
+  --pink: #ea76cb;
+  color: #ea76cb;
+  border: 1px solid rgb(1, 2, 3);
+  color: var(--pink);
diff --git a/web/src/screens/Foo.tsx b/web/src/screens/Foo.tsx
--- a/web/src/screens/Foo.tsx
+++ b/web/src/screens/Foo.tsx
@@ -1,1 +1,3 @@
+  <div style={{ marginTop: "12px" }} />
+  <div style={{ color: "#fff" }} />
+  <div className="row" />
`

func TestDesignCheckFlagsRawColorsAndMagicPixelsOnAddedLinesOnly(t *testing.T) {
	e, _ := testEnv(t)
	p := filepath.Join(t.TempDir(), "d.diff")
	write(t, p, designDiff)
	got, code := runJSON(t, e, "design-check", "--diff", p)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var where []string
	for _, f := range got["findings"].([]any) {
		m := f.(map[string]any)
		where = append(where, m["file"].(string)+":"+strconv.Itoa(int(m["line"].(float64))))
	}
	want := []string{
		"internal/tui/view.go:2",
		"internal/tui/view.go:4",
		"web/src/theme.css:2",
		"web/src/theme.css:3",
		"web/src/screens/Foo.tsx:1",
		"web/src/screens/Foo.tsx:2",
	}
	if strings.Join(where, " ") != strings.Join(want, " ") {
		t.Errorf("findings at\n%v\nwant\n%v", where, want)
	}
	if !strings.Contains(problemText(got), "theme") {
		t.Errorf("hint does not point at the theme: %s", problemText(got))
	}
}

func TestDesignCheckCleanDiffIsOK(t *testing.T) {
	e, _ := testEnv(t)
	p := filepath.Join(t.TempDir(), "d.diff")
	write(t, p, "diff --git a/web/src/a.css b/web/src/a.css\n--- a/web/src/a.css\n+++ b/web/src/a.css\n@@ -1,1 +1,2 @@\n+  color: var(--text);\n")
	got, code := runJSON(t, e, "design-check", "--diff", p)
	if code != 0 {
		t.Errorf("exit %d: %s", code, problemText(got))
	}
	if got["ui_change"] != true {
		t.Error("a web/src change is a UI change")
	}
}
