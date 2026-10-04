package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type finding struct {
	file string
	line int
}

const bannedMsg = "comments are banned; delete it (ADR 0044)"

func (f finding) String() string { return fmt.Sprintf("%s:%d: %s", f.file, f.line, bannedMsg) }

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	os.Exit(run(roots, os.Stderr))
}

func run(roots []string, out io.Writer) int {
	failed := false
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if d.IsDir() {
				if rel != "." && skipDir(rel, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && inTestdata(rel) {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			findings, err := checkFile(path, src)
			if err != nil {
				return err
			}
			for _, f := range findings {
				fmt.Fprintln(out, f)
				failed = true
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(out, err)
			return 2
		}
	}
	if failed {
		return 1
	}
	return 0
}

func skipDir(rel, name string) bool {
	if strings.HasPrefix(name, ".") && name != ".github" {
		return true
	}
	if name == "vendor" || name == "node_modules" || strings.HasPrefix(name, "_") {
		return true
	}
	return rel == "bin" || name == "dist"
}

func inTestdata(rel string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), "testdata")
}

type language int

const (
	other language = iota
	golang
	shell
	makefile
	yaml
	txtar
	lua
	sql
	script
	css
)

func languageOf(name string, src []byte) language {
	base := filepath.Base(name)
	switch filepath.Ext(base) {
	case ".go":
		return golang
	case ".sh", ".bash":
		return shell
	case ".yml", ".yaml":
		return yaml
	case ".txtar":
		return txtar
	case ".lua":
		return lua
	case ".sql":
		return sql
	case ".mk":
		return makefile
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		return script
	case ".css":
		return css
	case "":
		if base == "Makefile" {
			return makefile
		}
		first, _, _ := bytes.Cut(src, []byte("\n"))
		if bytes.HasPrefix(first, []byte("#!")) && bytes.Contains(first, []byte("sh")) {
			return shell
		}
	}
	return other
}

func checkFile(name string, src []byte) ([]finding, error) {
	switch languageOf(name, src) {
	case golang:
		return checkGo(name, src)
	case shell:
		return checkLines(name, src, shellComment, shellDirective, nil), nil
	case makefile:
		return checkLines(name, src, makeComment, nil, nil), nil
	case yaml:
		return checkLines(name, src, spacedHashComment, yamlDirective, nil), nil
	case txtar:
		return checkLines(name, src, spacedHashComment, nil, txtarFileHeader), nil
	case lua:
		return checkLines(name, src, luaComment, nil, nil), nil
	case sql:
		return checkSQL(name, src), nil
	case script:
		return checkScript(name, src, true), nil
	case css:
		return checkScript(name, src, false), nil
	}
	return nil, nil
}

var (
	goDirective      = regexp.MustCompile(`^//(go:\S|line |export |nolint:\S+$)`)
	generatedPattern = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	shellDirective   = regexp.MustCompile(`^(#!|\s*# shellcheck )`)
	yamlDirective    = regexp.MustCompile(`^\s*# yaml-language-server:`)
	txtarFileHeader  = regexp.MustCompile(`^-- \S.* --$`)
	scriptDirective  = regexp.MustCompile(`^/// <reference \S`)
)

func checkGo(name string, src []byte) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var findings []finding
	for _, group := range file.Comments {
		for _, c := range group.List {
			if goDirective.MatchString(c.Text) || generatedPattern.MatchString(c.Text) {
				continue
			}
			findings = append(findings, finding{name, fset.PositionFor(c.Pos(), false).Line})
		}
	}
	return findings, nil
}

func checkLines(name string, src []byte, hasComment func(string) bool, directive, stop *regexp.Regexp) []finding {
	var findings []finding
	for i, line := range strings.Split(string(src), "\n") {
		if stop != nil && stop.MatchString(line) {
			break
		}
		if directive != nil && directive.MatchString(line) {
			continue
		}
		if hasComment(line) {
			findings = append(findings, finding{name, i + 1})
		}
	}
	return findings
}

func shellComment(line string) bool { return quotedScan(line, "#", " \t;&|()<>") }

func spacedHashComment(line string) bool { return quotedScan(line, "#", " \t") }

func luaComment(line string) bool { return quotedScan(line, "--", "") }

func makeComment(line string) bool {
	if strings.HasPrefix(line, "\t") {
		return shellComment(line)
	}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '#':
			return true
		}
	}
	return false
}

func quotedScan(line, marker, startsWord string) bool {
	const ansiC = '$'
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			}
		case ch == '\\':
			i++
		case quote == ansiC || quote == '"':
			if (quote == ansiC && ch == '\'') || (quote == '"' && ch == '"') {
				quote = 0
			}
		case ch == '\'' && i > 0 && line[i-1] == '$':
			quote = ansiC
		case ch == '\'' || ch == '"':
			quote = ch
		case strings.HasPrefix(line[i:], marker):
			if startsWord == "" || i == 0 || strings.IndexByte(startsWord, line[i-1]) >= 0 {
				return true
			}
		}
	}
	return false
}

func checkSQL(name string, src []byte) []finding {
	var findings []finding
	line := 1
	report := func() {
		if len(findings) == 0 || findings[len(findings)-1].line != line {
			findings = append(findings, finding{name, line})
		}
	}
	var quote byte
	inBlock := false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if ch == '\n' {
			line++
			continue
		}
		rest := src[i:]
		switch {
		case inBlock:
			if bytes.HasPrefix(rest, []byte("*/")) {
				inBlock = false
				i++
			}
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '[':
			quote = ']'
		case bytes.HasPrefix(rest, []byte("--")):
			report()
			for i+1 < len(src) && src[i+1] != '\n' {
				i++
			}
		case bytes.HasPrefix(rest, []byte("/*")):
			report()
			inBlock = true
			i++
		}
	}
	return findings
}
