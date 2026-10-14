package domain

import (
	"strings"
	"testing"
)

func TestAutomationTOMLWritesOnlySetFields(t *testing.T) {
	a := Automation{
		Name: "fix-ci", Agent: "claude", Effort: "high", Prompt: "Fix \"it\".\nThen {{context}}",
		On: []string{"pr.ci_failed"}, Filter: AutomationFilter{Branch: "feat/*"}, Enabled: false,
	}
	want := "[[automation]]\n" +
		"name = \"fix-ci\"\n" +
		"agent = \"claude\"\n" +
		"effort = \"high\"\n" +
		"prompt = \"Fix \\\"it\\\".\\nThen {{context}}\"\n" +
		"on = [\"pr.ci_failed\"]\n" +
		"enabled = false\n" +
		"\n[automation.filter]\n" +
		"branch = \"feat/*\"\n"
	if got := AutomationTOML(a); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetAutomationBlock(t *testing.T) {
	deps := Automation{Name: "deps", Agent: "codex", Prompt: "Update.", Enabled: true}
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"append to an empty file", "", AutomationTOML(deps)},
		{"append after other tables",
			"# mine\n[launcher]\nmax_parallel = 2\n",
			"# mine\n[launcher]\nmax_parallel = 2\n\n" + AutomationTOML(deps)},
		{"replace the block with the same name and keep the rest",
			"[launcher]\nmax_parallel = 2\n\n[[automation]]\nname = \"deps\"\nagent = \"claude\"\nprompt = \"old\"\n\n[automation.filter]\nrepo = \"api\"\n\n[[automation]]\nname = \"other\"\nagent = \"claude\"\nprompt = \"x\"\n",
			"[launcher]\nmax_parallel = 2\n\n" + AutomationTOML(deps) + "\n[[automation]]\nname = \"other\"\nagent = \"claude\"\nprompt = \"x\"\n"},
		{"replace the last block",
			"[[automation]]\nname = \"other\"\nagent = \"claude\"\nprompt = \"x\"\n\n[[automation]]\nname = 'deps'\nprompt = \"old\"\n",
			"[[automation]]\nname = \"other\"\nagent = \"claude\"\nprompt = \"x\"\n\n" + AutomationTOML(deps)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SetAutomationBlock(tc.src, deps); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestSetAutomationBlockLeavesAnotherNameAlone(t *testing.T) {
	src := "[[automation]]\nname = \"deps-old\"\nprompt = \"x\"\n"
	got := SetAutomationBlock(src, Automation{Name: "deps", Agent: "claude", Prompt: "y", Enabled: true})
	if !strings.HasPrefix(got, src) || strings.Count(got, "[[automation]]") != 2 {
		t.Errorf("got:\n%s", got)
	}
}
