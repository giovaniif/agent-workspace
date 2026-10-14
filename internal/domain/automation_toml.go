package domain

import (
	"fmt"
	"strings"
)

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func AutomationTOML(a Automation) string {
	var b strings.Builder
	b.WriteString("[[automation]]\n")
	field := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s = %s\n", key, tomlString(value))
		}
	}
	field("name", a.Name)
	field("agent", a.Agent)
	field("model", a.Model)
	field("effort", a.Effort)
	field("target", a.Target)
	field("prompt", a.Prompt)
	field("schedule", a.Schedule)
	field("timezone", a.Timezone)
	if len(a.On) > 0 {
		quoted := make([]string, len(a.On))
		for i, ev := range a.On {
			quoted[i] = tomlString(ev)
		}
		fmt.Fprintf(&b, "on = [%s]\n", strings.Join(quoted, ", "))
	}
	field("precheck", a.Precheck)
	field("session", a.Session)
	field("browser", a.Browser)
	if !a.Enabled {
		b.WriteString("enabled = false\n")
	}
	if a.Filter != (AutomationFilter{}) {
		b.WriteString("\n[automation.filter]\n")
		field("repo", a.Filter.Repo)
		field("branch", a.Filter.Branch)
		field("author", a.Filter.Author)
	}
	return b.String()
}

func blockName(lines []string) string {
	for _, l := range lines {
		key, value, found := strings.Cut(strings.TrimSpace(l), "=")
		if found && strings.TrimSpace(key) == "name" {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func SetAutomationBlock(src string, a Automation) string {
	lines := strings.SplitAfter(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for start := 0; start < len(lines); start++ {
		if strings.TrimSpace(lines[start]) != "[[automation]]" {
			continue
		}
		end := start + 1
		for end < len(lines) {
			t := strings.TrimSpace(lines[end])
			if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[automation.") {
				break
			}
			end++
		}
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		if blockName(lines[start+1:end]) != a.Name {
			continue
		}
		return strings.Join(lines[:start], "") + AutomationTOML(a) + strings.Join(lines[end:], "")
	}
	if src == "" {
		return AutomationTOML(a)
	}
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return src + "\n" + AutomationTOML(a)
}
