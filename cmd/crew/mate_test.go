package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestPrintMatesListsEachMateAndTheProblems(t *testing.T) {
	list := rpc.MateList{
		Mates: []domain.Mate{
			{Name: "reviewer", Agent: "claude", Prompt: "x", Repos: []string{"api", "web"}, Owner: "U012ABCDEF", Enabled: true},
			{Name: "builder", Agent: "codex", Prompt: "x", Enabled: false},
		},
		Problems: []string{"/h/config.toml: mate \"x\": agent must be one of [claude codex]"},
	}
	var out bytes.Buffer
	printMates(&out, list)
	want := "NAME      AGENT   REPOS    OWNER       ENABLED\n" +
		"reviewer  claude  api,web  U012ABCDEF  yes\n" +
		"builder   codex   -        -           no\n" +
		"problem: /h/config.toml: mate \"x\": agent must be one of [claude codex]\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPrintMatesWithoutAnyTellsWhereToDefineThem(t *testing.T) {
	var out bytes.Buffer
	printMates(&out, rpc.MateList{})
	if !strings.Contains(out.String(), "[[mate]]") || !strings.Contains(out.String(), "crew mate set") {
		t.Errorf("got %q", out.String())
	}
}

func TestPrintMateShowsEveryFieldAndThePrompt(t *testing.T) {
	m := domain.Mate{
		Name: "reviewer", Agent: "claude", Model: "opus", Effort: "high", Repos: []string{"api", "web"},
		Owner: "U012ABCDEF", Enabled: true, Prompt: "Review code.\nBe brief.",
	}
	var out bytes.Buffer
	printMate(&out, m)
	for _, line := range []string{
		"name:    reviewer", "agent:   claude", "model:   opus", "effort:  high",
		"repos:   api,web", "owner:   U012ABCDEF", "enabled: yes",
		"\nprompt:\n  Review code.\n  Be brief.\n",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out.String())
		}
	}
}

func TestRunMateRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"show"}, {"remove"}, {"set"}, {"list", "x"}, {"show", "a", "b"}, {"remove", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := runMate(args, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: crew mate") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut.String())
		}
	}
}

func TestMateFromFlags(t *testing.T) {
	existing := domain.Mate{Name: "reviewer", Prompt: "old", Agent: "claude", Model: "opus", Repos: []string{"api"}, Owner: "U012ABCDEF", Enabled: true}
	cases := []struct {
		name string
		base domain.Mate
		args []string
		want domain.Mate
	}{
		{"new mate is enabled", domain.Mate{Name: "scout"}, []string{"--agent", "codex", "--prompt", "Look."},
			domain.Mate{Name: "scout", Agent: "codex", Prompt: "Look.", Enabled: true}},
		{"flags change only what they name", existing, []string{"--prompt", "new"},
			domain.Mate{Name: "reviewer", Prompt: "new", Agent: "claude", Model: "opus", Repos: []string{"api"}, Owner: "U012ABCDEF", Enabled: true}},
		{"repos replace the list", existing, []string{"--repo", "web", "--repo", "api"},
			domain.Mate{Name: "reviewer", Prompt: "old", Agent: "claude", Model: "opus", Repos: []string{"web", "api"}, Owner: "U012ABCDEF", Enabled: true}},
		{"disable", existing, []string{"--disable"},
			domain.Mate{Name: "reviewer", Prompt: "old", Agent: "claude", Model: "opus", Repos: []string{"api"}, Owner: "U012ABCDEF"}},
		{"enable again", domain.Mate{Name: "a"}, []string{"--enable"}, domain.Mate{Name: "a", Enabled: true}},
		{"empty model clears it", existing, []string{"--model", ""},
			domain.Mate{Name: "reviewer", Prompt: "old", Agent: "claude", Repos: []string{"api"}, Owner: "U012ABCDEF", Enabled: true}},
		{"no repo clears the list", existing, []string{"--no-repos"},
			domain.Mate{Name: "reviewer", Prompt: "old", Agent: "claude", Model: "opus", Owner: "U012ABCDEF", Enabled: true}},
		{"model, effort and owner", domain.Mate{Name: "a"}, []string{"--model", "m", "--effort", "high", "--owner", "U012ABCDEF"},
			domain.Mate{Name: "a", Model: "m", Effort: "high", Owner: "U012ABCDEF", Enabled: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mateFromFlags(tc.base, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMateFromFlagsRefusesBadFlags(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--enable", "--disable"}, {"--repo", "a", "--no-repos"}, {"stray"}} {
		if _, err := mateFromFlags(domain.Mate{Name: "a"}, args); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}
