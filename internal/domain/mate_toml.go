package domain

import (
	"fmt"
	"strings"
)

func MateTOML(m Mate) string {
	var b strings.Builder
	b.WriteString("[[mate]]\n")
	field := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s = %s\n", key, tomlString(value))
		}
	}
	field("name", m.Name)
	field("prompt", m.Prompt)
	field("agent", m.Agent)
	field("model", m.Model)
	field("effort", m.Effort)
	if len(m.Repos) > 0 {
		quoted := make([]string, len(m.Repos))
		for i, r := range m.Repos {
			quoted[i] = tomlString(r)
		}
		fmt.Fprintf(&b, "repos = [%s]\n", strings.Join(quoted, ", "))
	}
	field("owner", m.Owner)
	if !m.Enabled {
		b.WriteString("enabled = false\n")
	}
	return b.String()
}

type mateSpan struct{ start, end int }

func splitConfigLines(src string) []string {
	lines := strings.SplitAfter(src, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func findMateBlock(lines []string, name string) (mateSpan, bool) {
	for start := 0; start < len(lines); start++ {
		if strings.TrimSpace(lines[start]) != "[[mate]]" {
			continue
		}
		end := start + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "[") {
			end++
		}
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		if blockName(lines[start+1:end]) == name {
			return mateSpan{start, end}, true
		}
	}
	return mateSpan{}, false
}

func SetMateBlock(src string, m Mate) string {
	if span, ok := findMateBlock(splitConfigLines(src), m.Name); ok {
		lines := splitConfigLines(src)
		return strings.Join(lines[:span.start], "") + MateTOML(m) + strings.Join(lines[span.end:], "")
	}
	if src == "" {
		return MateTOML(m)
	}
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return src + "\n" + MateTOML(m)
}

func RemoveMateBlock(src, name string) string {
	lines := splitConfigLines(src)
	span, ok := findMateBlock(lines, name)
	if !ok {
		return src
	}
	rest := span.end
	for rest < len(lines) && strings.TrimSpace(lines[rest]) == "" {
		rest++
	}
	head := lines[:span.start]
	if rest == len(lines) {
		for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
			head = head[:len(head)-1]
		}
	}
	return strings.Join(head, "") + strings.Join(lines[rest:], "")
}
