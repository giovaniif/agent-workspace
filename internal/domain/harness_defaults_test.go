package domain

import "testing"

func TestDefaultLabelNamesTheValueThatDefaultMeans(t *testing.T) {
	cases := []struct{ value, want string }{
		{"opus", "default (opus)"},
		{"  high ", "default (high)"},
		{"", "default"},
	}
	for _, c := range cases {
		if got := DefaultLabel(c.value); got != c.want {
			t.Errorf("DefaultLabel(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestResolveDefaultPrefersTheHarnessConfigOverTheBuiltIn(t *testing.T) {
	cases := []struct {
		name                  string
		harness               Harness
		model, effort         string
		wantModel, wantEffort string
	}{
		{"claude with no config", HarnessClaude, "", "", "opus", "high"},
		{"codex with no config", HarnessCodex, "", "", "gpt-6.1-sol", "medium"},
		{"configured values win", HarnessCodex, "gpt-5.5", "xhigh", "gpt-5.5", "xhigh"},
		{"only the model configured", HarnessClaude, "sonnet", "", "sonnet", "high"},
		{"omp has no built-in default", HarnessOmp, "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model, effort := ResolveDefault(c.harness, c.model, c.effort)
			if model != c.wantModel || effort != c.wantEffort {
				t.Errorf("ResolveDefault = %q, %q; want %q, %q", model, effort, c.wantModel, c.wantEffort)
			}
		})
	}
}
