package domain

import (
	"reflect"
	"strings"
	"testing"
)

func validAutomation() Automation {
	return Automation{Name: "fix-ci", Agent: "claude", Target: "new-worktree", Prompt: "Fix {{work_item}}", Enabled: true}
}

func TestValidateAutomation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Automation)
		want string
	}{
		{"valid", func(*Automation) {}, ""},
		{"empty target means new worktree", func(a *Automation) { a.Target = "" }, ""},
		{"workspace target", func(a *Automation) { a.Target = "workspace" }, ""},
		{"worktree target with path", func(a *Automation) { a.Target = "worktree:/w/api" }, ""},
		{"reuse session", func(a *Automation) { a.Session = "reuse" }, ""},
		{"fresh session", func(a *Automation) { a.Session = "fresh" }, ""},
		{"known effort", func(a *Automation) { a.Effort = "high" }, ""},
		{"known events", func(a *Automation) { a.On = []string{"pr.ci_failed", "session.finished"} }, ""},
		{"every variable", func(a *Automation) {
			a.Prompt = "{{context}} {{work_item}} {{repo}} {{branch}} {{event}}"
		}, ""},
		{"no name", func(a *Automation) { a.Name = "" }, "name"},
		{"blank name", func(a *Automation) { a.Name = "  " }, "name"},
		{"name with space", func(a *Automation) { a.Name = "fix ci" }, "name"},
		{"unknown agent", func(a *Automation) { a.Agent = "gemini" }, "agent"},
		{"no agent", func(a *Automation) { a.Agent = "" }, "agent"},
		{"unknown effort", func(a *Automation) { a.Effort = "huge" }, "effort"},
		{"unknown target", func(a *Automation) { a.Target = "somewhere" }, "target"},
		{"worktree target without path", func(a *Automation) { a.Target = "worktree:" }, "target"},
		{"unknown session", func(a *Automation) { a.Session = "always" }, "session"},
		{"empty prompt", func(a *Automation) { a.Prompt = " " }, "prompt"},
		{"unknown variable", func(a *Automation) { a.Prompt = "Fix {{ticket}}" }, "{{ticket}}"},
		{"unknown event", func(a *Automation) { a.On = []string{"pr.closed"} }, "pr.closed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validAutomation()
			tc.edit(&a)
			err := ValidateAutomation(a)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("got %v, want no error", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error about %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), a.Name) {
				t.Errorf("error %q does not name the automation %q", err, a.Name)
			}
		})
	}
}

func TestMergeAutomationsRepoReplacesGlobalByName(t *testing.T) {
	global := []Automation{
		{Name: "triage", Agent: "codex", Prompt: "global triage", Source: "global"},
		{Name: "deps", Agent: "claude", Prompt: "global deps", Model: "opus", Source: "global"},
	}
	repo := []Automation{
		{Name: "deps", Agent: "claude", Prompt: "repo deps", Source: "/w/api"},
		{Name: "audit", Agent: "claude", Prompt: "repo audit", Source: "/w/api"},
	}
	got := MergeAutomations(global, repo)
	want := []Automation{
		{Name: "audit", Agent: "claude", Prompt: "repo audit", Source: "/w/api"},
		{Name: "deps", Agent: "claude", Prompt: "repo deps", Source: "/w/api"},
		{Name: "triage", Agent: "codex", Prompt: "global triage", Source: "global"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMergeAutomationsWithNoRepoKeepsGlobalSorted(t *testing.T) {
	got := MergeAutomations([]Automation{{Name: "b"}, {Name: "a"}}, nil)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("got %+v", got)
	}
}

func TestExpandPrompt(t *testing.T) {
	vars := PromptVars{Context: "only the api", WorkItem: "#42", Repo: "api", Branch: "fix-ci", Event: "ci failed"}
	cases := []struct {
		name string
		tmpl string
		vars PromptVars
		want string
	}{
		{"all variables", "{{work_item}} in {{repo}} on {{branch}}: {{event}}. {{context}}", vars,
			"#42 in api on fix-ci: ci failed. only the api"},
		{"repeated variable", "{{repo}}/{{repo}}", PromptVars{Repo: "api"}, "api/api"},
		{"context appended when absent", "Fix the build.", PromptVars{Context: "look at lint"},
			"Fix the build.\n\nlook at lint"},
		{"context appended after trailing space trimmed", "Fix the build.\n\n", PromptVars{Context: "look at lint"},
			"Fix the build.\n\nlook at lint"},
		{"no context and no placeholder", "Fix the build.", PromptVars{}, "Fix the build."},
		{"blank context not appended", "Fix the build.", PromptVars{Context: "  \n"}, "Fix the build."},
		{"empty variable expands empty", "PR {{work_item}}.", PromptVars{}, "PR ."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandPrompt(tc.tmpl, tc.vars); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
