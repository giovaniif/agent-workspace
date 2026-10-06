package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCheckFile(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantLines []int
	}{
		{"x.go", "comments.go.txt", []int{1, 4, 7, 10, 13, 15, 16, 20}},
		{"x_test.go", "comments.go.txt", []int{1, 4, 7, 10, 13, 15, 16, 20}},
		{"x.go", "directives.go.txt", []int{20}},
		{"x.sh", "shell.sh.txt", []int{3, 5, 8, 10, 11}},
		{"x.lua", "lua.lua.txt", []int{2, 3, 5}},
		{"x.yml", "yaml.yml.txt", []int{3, 4}},
		{"x.yaml", "yaml.yml.txt", []int{3, 4}},
		{"x.txtar", "script.txtar.txt", []int{1}},
		{"x.sql", "query.sql.txt", []int{1, 2, 4, 5}},
		{"x.md", "shell.sh.txt", nil},
		{"tool", "shell.sh.txt", []int{3, 5, 8, 10, 11}},
		{"Makefile", "makefile.mk.txt", []int{1, 2, 6}},
		{"x.mk", "makefile.mk.txt", []int{1, 2, 6}},
		{"x.ts", "script.ts.txt", []int{2, 7, 9, 10}},
		{"x.mts", "script.ts.txt", []int{2, 7, 9, 10}},
		{"x.js", "script.ts.txt", []int{2, 7, 9, 10}},
		{"x.tsx", "component.tsx.txt", []int{7, 13}},
		{"x.css", "style.css.txt", []int{4, 9}},
		{"x.swift", "swift.swift.txt", []int{3, 5, 6, 13}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.fixture, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			findings, err := checkFile(tt.name, src)
			if err != nil {
				t.Fatal(err)
			}
			var lines []int
			for _, f := range findings {
				lines = append(lines, f.line)
			}
			if !slices.Equal(lines, tt.wantLines) {
				t.Errorf("finding lines = %v, want %v (%v)", lines, tt.wantLines, findings)
			}
		})
	}
}

func TestRunExitCode(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"clean tree", map[string]string{
			"a.go":         "//go:build integration\n\npackage a\n",
			"run.sh":       "#!/bin/sh\necho hi\n",
			"data/notes":   "# a heading, not a script\n",
			"README.md":    "# Title\n",
			"x.golden":     "# not code\n",
			"bin/agentws":  "\x7fELF # binary\n",
			"vendor/v.go":  "// vendored\npackage v\n",
			".git/hook.sh": "#!/bin/sh\n# git's own\n",
		}, 0},
		{"comment in a test file", map[string]string{"a_test.go": "package a\n\n// helper\nfunc h() {}\n"}, 1},
		{"workflow under .github", map[string]string{".github/workflows/ci.yml": "on: push\n# why: a reason\n"}, 1},
		{"script found by its shebang", map[string]string{"scripts/tool": "#!/usr/bin/env bash\n# usage: tool\n"}, 1},
		{"recipe comment in a Makefile", map[string]string{"Makefile": "build:\n\t# compile\n\tgo build\n"}, 1},
		{"fake binary in testdata", map[string]string{"test/testdata/bin/gh": "#!/bin/sh\n# fake gh\n"}, 1},
		{"e2e script in testdata", map[string]string{"testdata/script/a.txtar": "# checks a\nexec true\n"}, 1},
		{"comment in web source", map[string]string{"web/src/app.tsx": "// the app\nexport const a = 1;\n"}, 1},
		{"comment in Swift source", map[string]string{"macos/Sources/AgentwsKit/a.swift": "// the kit\nlet a = 1\n"}, 1},
		{"Swift build output", map[string]string{"macos/.build/x/a.swift": "// generated\n"}, 0},
		{"web dependencies", map[string]string{"web/node_modules/x/index.js": "/* vendored */\n"}, 0},
		{"built web app", map[string]string{"internal/serve/dist/assets/app.js": "/*! license */\nexport{};\n"}, 0},
		{"go fixture in testdata", map[string]string{"testdata/fixture.go": "// fixture input\npackage f\n"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := run([]string{dir}, os.Stderr); got != tt.want {
				t.Errorf("run exit = %d, want %d", got, tt.want)
			}
		})
	}
}
