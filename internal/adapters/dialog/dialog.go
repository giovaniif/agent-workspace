package dialog

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

const (
	maxFooter      = 4
	maxQuestionGap = 60
)

type TextStart int

const (
	FromRule TextStart = iota
	FromQuestion
)

type Spec struct {
	Questions []string
	Start     TextStart
	Keys      func(number int, shortcut string) []string
}

var (
	optionLine = regexp.MustCompile(`^([❯›>])?\s*(\d+)\.\s+(.+)$`)
	shortcut   = regexp.MustCompile(`\s+\(([^()\s]+)\)$`)
	knownKeys  = regexp.MustCompile(`^(esc|shift\+tab|ctrl\+[a-z]|[a-z])$`)
)

type option struct {
	number   int
	label    string
	shortcut string
	selected bool
}

func Parse(screen string, spec Spec) (app.PermissionPrompt, bool) {
	lines := normalize(screen)
	first, opts, ok := lastOptions(lines)
	if !ok || !oneSelected(opts) || footerLines(lines, first+len(opts)) > maxFooter {
		return app.PermissionPrompt{}, false
	}
	question, ok := findQuestion(lines, first, spec.Questions)
	if !ok {
		return app.PermissionPrompt{}, false
	}
	start := question
	if spec.Start == FromRule {
		start = afterRule(lines, question)
	}
	choices := make([]app.PermissionChoice, 0, len(opts))
	for _, o := range opts {
		choices = append(choices, app.PermissionChoice{
			ID:    strconv.Itoa(o.number),
			Label: o.label,
			Keys:  spec.Keys(o.number, o.shortcut),
		})
	}
	return app.PermissionPrompt{Text: text(lines[start:first]), Choices: choices}, true
}

func normalize(screen string) []string {
	rows := strings.Split(screen, "\n")
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.TrimSpace(strings.Trim(strings.TrimSpace(r), "│")))
	}
	return out
}

func parseOption(line string) (option, bool) {
	m := optionLine.FindStringSubmatch(line)
	if m == nil {
		return option{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return option{}, false
	}
	o := option{number: n, label: strings.TrimSpace(m[3]), selected: m[1] != ""}
	if s := shortcut.FindStringSubmatch(o.label); s != nil && knownKeys.MatchString(s[1]) {
		o.shortcut = s[1]
		o.label = strings.TrimSpace(strings.TrimSuffix(o.label, s[0]))
	}
	return o, true
}

func lastOptions(lines []string) (int, []option, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		o, ok := parseOption(lines[i])
		if !ok || o.number != 1 {
			continue
		}
		opts := []option{o}
		for j := i + 1; j < len(lines); j++ {
			next, ok := parseOption(lines[j])
			if !ok || next.number != len(opts)+1 {
				break
			}
			opts = append(opts, next)
		}
		if len(opts) >= 2 {
			return i, opts, true
		}
	}
	return 0, nil, false
}

func oneSelected(opts []option) bool {
	selected := 0
	for _, o := range opts {
		if o.selected {
			selected++
		}
	}
	return selected == 1
}

func footerLines(lines []string, from int) int {
	n := 0
	for _, l := range lines[from:] {
		if l != "" {
			n++
		}
	}
	return n
}

func findQuestion(lines []string, before int, prefixes []string) (int, bool) {
	for i := before - 1; i >= 0 && before-i <= maxQuestionGap; i-- {
		if !strings.HasSuffix(lines[i], "?") {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(lines[i], p) {
				return i, true
			}
		}
	}
	return 0, false
}

func afterRule(lines []string, question int) int {
	for i := question - 1; i >= 0 && question-i <= maxQuestionGap; i-- {
		if isRule(lines[i], '─') {
			return i + 1
		}
	}
	return question
}

func isRule(line string, r rune) bool {
	return len([]rune(line)) >= 3 && strings.Trim(line, string(r)) == ""
}

func text(lines []string) string {
	var kept []string
	for _, l := range lines {
		if isRule(l, '─') || isRule(l, '╌') {
			continue
		}
		if l == "" && (len(kept) == 0 || kept[len(kept)-1] == "") {
			continue
		}
		kept = append(kept, l)
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}
