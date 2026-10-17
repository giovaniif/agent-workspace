package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

type finding struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Rule string `json:"rule"`
	Text string `json:"text"`
}

var (
	rawLipgloss = regexp.MustCompile(`lipgloss\.Color\(\s*"`)
	hexColor    = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	rgbColor    = regexp.MustCompile(`\b(rgb|rgba|hsl|hsla)\(`)
	cssVarDecl  = regexp.MustCompile(`^\s*--[A-Za-z0-9-]+\s*:`)
	magicPx     = regexp.MustCompile(`\b\d+(\.\d+)?px\b`)
	hunkHeader  = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

func isTestPath(f string) bool {
	return strings.HasSuffix(f, "_test.go") || strings.Contains(f, ".test.") || strings.Contains(f, "/testdata/") || strings.HasPrefix(f, "web/src/test/")
}

func checkLine(file, line string) []string {
	if isTestPath(file) {
		return nil
	}
	var rules []string
	switch {
	case strings.HasPrefix(file, "internal/") && strings.HasSuffix(file, ".go"):
		if file == "internal/tui/theme.go" {
			return nil
		}
		if rawLipgloss.MatchString(line) {
			rules = append(rules, "raw lipgloss colour: use a Theme field from internal/tui/theme.go")
		} else if strings.HasPrefix(file, "internal/tui/") && regexp.MustCompile(`"#[0-9a-fA-F]{6}"`).MatchString(line) {
			rules = append(rules, "raw hex colour: add it to the Theme in internal/tui/theme.go")
		}
	case strings.HasPrefix(file, "web/src/") && strings.HasSuffix(file, ".css"):
		if cssVarDecl.MatchString(line) {
			return nil
		}
		if hexColor.MatchString(line) || rgbColor.MatchString(line) {
			rules = append(rules, "raw colour: use a var(--token) from web/src/theme.css")
		}
	case strings.HasPrefix(file, "web/src/") && (strings.HasSuffix(file, ".tsx") || strings.HasSuffix(file, ".ts")):
		if hexColor.MatchString(line) || rgbColor.MatchString(line) {
			rules = append(rules, "raw colour in code: use a class with a var(--token) from web/src/theme.css")
		}
		if magicPx.MatchString(line) {
			rules = append(rules, "magic px in code: put the size in a class in web/src/theme.css")
		}
	}
	return rules
}

func diffFindings(diff string) ([]finding, bool) {
	var out []finding
	file, n, ui := "", 0, false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			if file == "/dev/null" {
				file = ""
			}
			ui = ui || isUIFile(file)
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "diff --git "):
			if strings.HasPrefix(line, "diff --git ") {
				parts := strings.Fields(line)
				if len(parts) == 4 {
					ui = ui || isUIFile(strings.TrimPrefix(parts[3], "b/"))
				}
			}
		case strings.HasPrefix(line, "@@"):
			if m := hunkHeader.FindStringSubmatch(line); m != nil {
				n, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(line, "+"):
			for _, rule := range checkLine(file, line[1:]) {
				out = append(out, finding{file, n, rule, strings.TrimSpace(line[1:])})
			}
			n++
		case strings.HasPrefix(line, "-"):
		default:
			n++
		}
	}
	return out, ui
}

func designCheck(e *env, args []string, r *result) {
	o, ok := parse(args, []string{"base", "diff", "repo"}, nil, r)
	if !ok {
		return
	}
	var diff string
	if p := o.get("diff", ""); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			r.fail("cannot read "+p, "give a unified diff file")
			return
		}
		diff = string(b)
	} else {
		repo := o.get("repo", e.repo)
		base := o.get("base", "origin/main")
		mb, ok := e.git(repo, "merge-base", base, "HEAD")
		if !ok {
			r.fail("no merge-base with "+base, "git fetch origin main")
			return
		}
		out, _, code := e.sys.exec(cmd{dir: repo, name: "git", args: []string{"diff", "-U0", "--no-color", "-M", mb}})
		if code != 0 {
			r.fail("git diff failed", "run from the crew checkout")
			return
		}
		diff = out
	}
	fs, ui := diffFindings(diff)
	if fs == nil {
		fs = []finding{}
	}
	r.set("findings", fs)
	r.set("ui_change", ui)
	for _, f := range fs {
		r.fail(f.File+":"+strconv.Itoa(f.Line)+": "+f.Text, f.Rule)
	}
}
