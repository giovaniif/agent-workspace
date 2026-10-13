package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Automation struct {
	Name     string   `json:"name"`
	Agent    string   `json:"agent"`
	Model    string   `json:"model,omitempty"`
	Effort   string   `json:"effort,omitempty"`
	Target   string   `json:"target,omitempty"`
	Prompt   string   `json:"prompt"`
	Schedule string   `json:"schedule,omitempty"`
	Timezone string   `json:"timezone,omitempty"`
	On       []string `json:"on,omitempty"`
	Precheck string   `json:"precheck,omitempty"`
	Session  string   `json:"session,omitempty"`
	Enabled  bool     `json:"enabled"`
	Source   string   `json:"source"`
}

const (
	AutomationTargetNewWorktree = "new-worktree"
	AutomationTargetWorkspace   = "workspace"
	AutomationTargetWorktree    = "worktree:"
	AutomationSessionFresh      = "fresh"
	AutomationSessionReuse      = "reuse"
	AutomationSourceGlobal      = "global"
)

var AutomationEvents = []string{
	"pr.opened", "pr.ci_failed", "pr.review_requested", "pr.merged",
	"session.finished", "session.needs_attention", "worktree.created",
}

var promptVariables = []string{"context", "work_item", "repo", "branch", "event"}

var promptVariable = regexp.MustCompile(`\{\{\s*([a-zA-Z_]+)\s*\}\}`)

type PromptVars struct {
	Context  string
	WorkItem string
	Repo     string
	Branch   string
	Event    string
}

func ValidateAutomation(a Automation) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("automation %q: %s", a.Name, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(a.Name) == "" || strings.ContainsAny(a.Name, " \t\n") {
		return fail("name must be one word with no spaces")
	}
	h := Harness(a.Agent)
	if !slices.Contains(Harnesses(), h) {
		return fail("agent must be one of %v", Harnesses())
	}
	if a.Effort != "" && !slices.Contains(Spec(h).Efforts, a.Effort) {
		return fail("effort %q is not one of %v", a.Effort, Spec(h).Efforts)
	}
	if !validTarget(a.Target) {
		return fail("target %q must be new-worktree, workspace or worktree:<path>", a.Target)
	}
	if a.Session != "" && a.Session != AutomationSessionFresh && a.Session != AutomationSessionReuse {
		return fail("session %q must be fresh or reuse", a.Session)
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return fail("prompt is empty")
	}
	for _, m := range promptVariable.FindAllStringSubmatch(a.Prompt, -1) {
		if !slices.Contains(promptVariables, m[1]) {
			return fail("prompt uses the unknown variable %s", m[0])
		}
	}
	if a.Schedule != "" {
		if _, err := ParseSchedule(a.Schedule); err != nil {
			return fail("%v", err)
		}
	}
	for _, ev := range a.On {
		if !slices.Contains(AutomationEvents, ev) {
			return fail("event %q is not one of %v", ev, AutomationEvents)
		}
	}
	return nil
}

func validTarget(t string) bool {
	switch {
	case t == "", t == AutomationTargetNewWorktree, t == AutomationTargetWorkspace:
		return true
	case strings.HasPrefix(t, AutomationTargetWorktree):
		return strings.TrimSpace(strings.TrimPrefix(t, AutomationTargetWorktree)) != ""
	}
	return false
}

func MergeAutomations(global, repo []Automation) []Automation {
	byName := map[string]Automation{}
	for _, a := range global {
		byName[a.Name] = a
	}
	for _, a := range repo {
		byName[a.Name] = a
	}
	out := make([]Automation, 0, len(byName))
	for _, a := range byName {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y Automation) int { return strings.Compare(x.Name, y.Name) })
	return out
}

func ExpandPrompt(tmpl string, v PromptVars) string {
	values := map[string]string{
		"context": v.Context, "work_item": v.WorkItem, "repo": v.Repo, "branch": v.Branch, "event": v.Event,
	}
	hasContext := false
	out := promptVariable.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := promptVariable.FindStringSubmatch(m)[1]
		if name == "context" {
			hasContext = true
		}
		if val, ok := values[name]; ok {
			return val
		}
		return m
	})
	if !hasContext && strings.TrimSpace(v.Context) != "" {
		out = strings.TrimRight(out, " \t\n") + "\n\n" + strings.TrimSpace(v.Context)
	}
	return out
}
