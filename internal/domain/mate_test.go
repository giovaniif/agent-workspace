package domain

import (
	"strings"
	"testing"
)

var mateWorkspaces = []Workspace{
	{Root: "/w/shop", Kind: WorkspaceOrchestration, Repos: []Repo{{Name: "api", Path: "/w/shop/api"}, {Name: "web", Path: "/w/shop/web"}}},
	{Root: "/w/solo", Kind: WorkspaceSingle, Repos: []Repo{{Name: "solo", Path: "/w/solo"}}},
}

func validMate() Mate {
	return Mate{Name: "reviewer", Prompt: "Review code.", Agent: "claude", Repos: []string{"api"}, Owner: "U012ABCDEF", Enabled: true}
}

func TestValidateMate(t *testing.T) {
	cases := []struct {
		name    string
		edit    func(*Mate)
		wantErr string
	}{
		{"valid", func(*Mate) {}, ""},
		{"codex", func(m *Mate) { m.Agent = "codex" }, ""},
		{"model and effort", func(m *Mate) { m.Model = "opus"; m.Effort = "high" }, ""},
		{"owner not set yet", func(m *Mate) { m.Owner = "" }, ""},
		{"workspace user id", func(m *Mate) { m.Owner = "W012ABCDEF" }, ""},
		{"repo by path", func(m *Mate) { m.Repos = []string{"/w/shop/web"} }, ""},
		{"repo of a single workspace", func(m *Mate) { m.Repos = []string{"solo"} }, ""},
		{"no repos", func(m *Mate) { m.Repos = nil }, ""},
		{"empty name", func(m *Mate) { m.Name = "" }, "name"},
		{"name with a space", func(m *Mate) { m.Name = "my mate" }, "name"},
		{"name with upper case", func(m *Mate) { m.Name = "Reviewer" }, "name"},
		{"name starts with a dash", func(m *Mate) { m.Name = "-x" }, "name"},
		{"name too long", func(m *Mate) { m.Name = strings.Repeat("a", 33) }, "name"},
		{"name at the limit", func(m *Mate) { m.Name = strings.Repeat("a", 32) }, ""},
		{"empty prompt", func(m *Mate) { m.Prompt = "  \n" }, "prompt"},
		{"unknown harness", func(m *Mate) { m.Agent = "gemini" }, "agent"},
		{"omp is not a mate harness", func(m *Mate) { m.Agent = "omp" }, "agent"},
		{"no harness", func(m *Mate) { m.Agent = "" }, "agent"},
		{"unknown effort", func(m *Mate) { m.Effort = "turbo" }, "effort"},
		{"owner in lower case", func(m *Mate) { m.Owner = "u012abcdef" }, "owner"},
		{"owner too short", func(m *Mate) { m.Owner = "U01" }, "owner"},
		{"owner is a handle", func(m *Mate) { m.Owner = "@giovani" }, "owner"},
		{"owner is a channel id", func(m *Mate) { m.Owner = "C012ABCDEF" }, "owner"},
		{"unknown repo", func(m *Mate) { m.Repos = []string{"api", "billing"} }, "billing"},
		{"empty repo", func(m *Mate) { m.Repos = []string{""} }, "repo"},
		{"repo twice", func(m *Mate) { m.Repos = []string{"api", "/w/shop/api"} }, "twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMate()
			tc.edit(&m)
			err := ValidateMate(m, mateWorkspaces)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("got %v, want valid", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("got no error, want one about %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("got %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateMateNamesTheMate(t *testing.T) {
	m := validMate()
	m.Agent = "gemini"
	if err := ValidateMate(m, mateWorkspaces); err == nil || !strings.Contains(err.Error(), `mate "reviewer"`) {
		t.Errorf("got %v", err)
	}
}

func TestCheckMatesKeepsTheValidOnesAndReportsTheRest(t *testing.T) {
	good, other := validMate(), validMate()
	other.Name = "builder"
	bad := validMate()
	bad.Name = "broken"
	bad.Agent = "gemini"
	dup := validMate()
	dup.Prompt = "A second reviewer."
	got, problems := CheckMates([]Mate{good, bad, other, dup}, mateWorkspaces)
	if len(got) != 2 || got[0].Name != "reviewer" || got[1].Name != "builder" || got[0].Prompt != "Review code." {
		t.Errorf("mates %+v", got)
	}
	if len(problems) != 2 || !strings.Contains(problems[0].Error(), "broken") || !strings.Contains(problems[1].Error(), "twice") {
		t.Errorf("problems %v", problems)
	}
}

func TestCheckMatesWithNoneIsEmpty(t *testing.T) {
	got, problems := CheckMates(nil, nil)
	if len(got) != 0 || len(problems) != 0 {
		t.Errorf("got %v, %v", got, problems)
	}
}

func TestMateTOMLWritesOnlySetFields(t *testing.T) {
	m := Mate{Name: "reviewer", Prompt: "Review \"it\".\nBe brief.", Agent: "codex", Effort: "high", Repos: []string{"api", "web"}, Owner: "U012ABCDEF"}
	want := "[[mate]]\n" +
		"name = \"reviewer\"\n" +
		"prompt = \"Review \\\"it\\\".\\nBe brief.\"\n" +
		"agent = \"codex\"\n" +
		"effort = \"high\"\n" +
		"repos = [\"api\", \"web\"]\n" +
		"owner = \"U012ABCDEF\"\n" +
		"enabled = false\n"
	if got := MateTOML(m); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	m.Enabled = true
	if got := MateTOML(m); strings.Contains(got, "enabled") {
		t.Errorf("an enabled mate writes no enabled key:\n%s", got)
	}
}

func TestSetMateBlock(t *testing.T) {
	m := Mate{Name: "reviewer", Prompt: "New.", Agent: "claude", Enabled: true}
	other := "[[mate]]\nname = \"other\"\nprompt = \"x\"\nagent = \"claude\"\n"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"append to an empty file", "", MateTOML(m)},
		{"append after other tables",
			"[launcher]\nmax_parallel = 2\n",
			"[launcher]\nmax_parallel = 2\n\n" + MateTOML(m)},
		{"append to a file with no final newline", "[x]\na = 1", "[x]\na = 1\n\n" + MateTOML(m)},
		{"replace the block with the same name and keep the rest",
			"[launcher]\nmax_parallel = 2\n\n[[mate]]\nname = \"reviewer\"\nprompt = \"old\"\nagent = \"codex\"\n\n" + other + "\n[tracker]\nx = 1\n",
			"[launcher]\nmax_parallel = 2\n\n" + MateTOML(m) + "\n" + other + "\n[tracker]\nx = 1\n"},
		{"replace the last block",
			other + "\n[[mate]]\nname = 'reviewer'\nprompt = \"old\"\n",
			other + "\n" + MateTOML(m)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SetMateBlock(tc.src, m); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestSetMateBlockLeavesAnotherNameAndAutomationsAlone(t *testing.T) {
	src := "[[mate]]\nname = \"reviewer-old\"\nprompt = \"x\"\n\n[[automation]]\nname = \"reviewer\"\nprompt = \"y\"\n"
	got := SetMateBlock(src, Mate{Name: "reviewer", Agent: "claude", Prompt: "z", Enabled: true})
	if !strings.HasPrefix(got, src) || strings.Count(got, "[[mate]]") != 2 || strings.Count(got, "[[automation]]") != 1 {
		t.Errorf("got:\n%s", got)
	}
}

func TestRemoveMateBlock(t *testing.T) {
	a := "[[mate]]\nname = \"a\"\nprompt = \"x\"\n"
	b := "[[mate]]\nname = \"b\"\nprompt = \"y\"\n"
	cases := []struct {
		name string
		src  string
		del  string
		want string
	}{
		{"only block", a, "a", ""},
		{"first of two", a + "\n" + b, "a", b},
		{"last of two", a + "\n" + b, "b", a},
		{"middle keeps the other tables",
			"[launcher]\nm = 1\n\n" + a + "\n[tracker]\nx = 1\n",
			"a", "[launcher]\nm = 1\n\n[tracker]\nx = 1\n"},
		{"last after another table", "[launcher]\nm = 1\n\n" + a, "a", "[launcher]\nm = 1\n"},
		{"unknown name changes nothing", a + "\n" + b, "c", a + "\n" + b},
		{"an automation of the same name stays",
			"[[automation]]\nname = \"a\"\nprompt = \"z\"\n\n" + a, "a",
			"[[automation]]\nname = \"a\"\nprompt = \"z\"\n"},
		{"empty file", "", "a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RemoveMateBlock(tc.src, tc.del); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}
