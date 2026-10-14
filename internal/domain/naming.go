package domain

import (
	"strings"
)

const summaryWords = 4

var (
	fillerWords = wordSet("please", "can", "could", "would", "you", "i", "we", "let's", "lets",
		"need", "want", "to", "help", "me", "hey", "hi", "just", "so", "also", "kindly")
	danglingWords = wordSet("the", "a", "an", "to", "of", "and", "or", "for", "in", "on", "with", "at", "by", "from", "when", "that", "is")
)

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

func NameFor(task Task, prs []PullRequest) string {
	candidates := []string{task.PinnedName}
	for _, pr := range prs {
		candidates = append(candidates, pr.Title)
	}
	candidates = append(candidates, task.PRTitle, task.IssueTitle, SummarizeText(task.Text))
	for _, c := range candidates {
		if name := strings.TrimSpace(c); name != "" {
			return name
		}
	}
	return ""
}

func SummarizeText(text string) string {
	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	words := strings.Fields(line)
	start := 0
	for start < len(words) && fillerWords[strings.ToLower(trimWord(words[start]))] {
		start++
	}
	if start == len(words) {
		start = 0
	}
	var kept []string
	for _, w := range words[start:] {
		kept = append(kept, trimWord(w))
		if len(kept) == summaryWords || endsSentence(w) {
			break
		}
	}
	for len(kept) > 2 && danglingWords[strings.ToLower(kept[len(kept)-1])] {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, " ")
}

func trimWord(w string) string { return strings.Trim(w, "\"'.,;:!?") }

func endsSentence(w string) bool {
	return strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?")
}

func PinName(task Task, name string) Task {
	task.PinnedName = strings.TrimSpace(name)
	return task
}

func WithTitle(task Task, title string) (Task, bool) {
	title = strings.TrimSpace(title)
	if title == "" {
		return task, false
	}
	if task.Source != TaskPR || task.PRTitle == title {
		return task, false
	}
	task.PRTitle = title
	return task, true
}
