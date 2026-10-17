package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type fakeGH struct {
	api      map[string]string
	calls    [][]string
	posted   []string
	merged   []string
	ledger   []string
	verifyOK bool
}

func (f *fakeGH) exec(c cmd) (string, string, int) {
	f.calls = append(f.calls, append([]string{c.name}, c.args...))
	if c.name == "go" {
		if f.verifyOK {
			return `{"ok":true,"head":"` + f.api["head"] + `","problems":[]}`, "", 0
		}
		return `{"ok":false,"head":"` + f.api["head"] + `","problems":[{"msg":"no head fix","hint":"x"}]}`, "", 1
	}
	if c.name != "gh" {
		return "", "unexpected " + c.name, 127
	}
	j := strings.Join(c.args, " ")
	switch {
	case strings.HasPrefix(j, "pr merge"):
		f.merged = append(f.merged, j)
		return "", "", 0
	case strings.Contains(j, "issues/") && strings.Contains(j, "/comments") && strings.Contains(j, "-X POST"):
		for i, a := range c.args {
			if a == "-f" && strings.HasPrefix(c.args[i+1], "body=") {
				body := strings.TrimPrefix(c.args[i+1], "body=")
				if strings.Contains(j, "issues/900/") {
					f.ledger = append(f.ledger, body)
				} else {
					f.posted = append(f.posted, body)
				}
			}
		}
		return "{}", "", 0
	case strings.HasPrefix(j, "pr list"):
		return f.api["prlist"], "", 0
	case strings.HasPrefix(j, "issue list"):
		return `[{"number":900}]`, "", 0
	case strings.HasPrefix(j, "api graphql"):
		return f.api["graphql"], "", 0
	case strings.HasPrefix(j, "api"):
		best := ""
		for k := range f.api {
			if strings.HasPrefix(k, "repos/") && strings.Contains(j, k) && len(k) > len(best) {
				best = k
			}
		}
		if best == "" {
			return "", "HTTP 404: Not Found", 1
		}
		return f.api[best], "", 0
	}
	return "", "unexpected gh " + j, 1
}

const headSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func greenPR() *fakeGH {
	return &fakeGH{api: map[string]string{
		"head":                               headSHA,
		"repos/giovaniif/crew/pulls/7/files": `[{"filename":"internal/tui/view.go","status":"modified","patch":"@@ -1 +1 @@\n-a\n+b"}]`,
		"repos/giovaniif/crew/pulls/7":       `{"number":7,"draft":false,"mergeable_state":"clean","head":{"sha":"` + headSHA + `"},"base":{"ref":"main"},"merged":false}`,
		"repos/giovaniif/crew/branches/main/protection":           `{"required_status_checks":{"contexts":["build","tdd","mutate","CodeRabbit"]}}`,
		"repos/giovaniif/crew/commits/" + headSHA + "/check-runs": `{"check_runs":[{"name":"build","status":"completed","conclusion":"success"},{"name":"tdd","status":"completed","conclusion":"success"},{"name":"mutate","status":"completed","conclusion":"success"}]}`,
		"repos/giovaniif/crew/commits/" + headSHA + "/status":     `{"statuses":[{"context":"CodeRabbit","state":"success"}]}`,
		"repos/giovaniif/crew/issues/7/comments":                  `[{"body":"looks good\n<!-- land:verdict v=1 sha=` + headSHA + ` verdict=PASS -->","user":{"login":"bot"}}]`,
		"graphql":                                                 `{"data":{"repository":{"pullRequest":{"reviewDecision":"","reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[{"isResolved":true}]}}}}}`,
	}, verifyOK: true}
}

func runJSON(t *testing.T, e *env, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	e.out = &out
	code := run(args, e)
	var got map[string]any
	dec := json.NewDecoder(&out)
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("%v: not one JSON object: %v: %q", args, err, out.String())
	}
	if dec.More() {
		t.Fatalf("%v: more than one JSON value", args)
	}
	if _, ok := got["ok"].(bool); !ok {
		t.Fatalf("no ok: %v", got)
	}
	return got, code
}

func flags(got map[string]any) string {
	var b strings.Builder
	list, _ := got["flags"].([]any)
	for _, f := range list {
		b.WriteString(f.(string))
		b.WriteString("\n")
	}
	return b.String()
}

func blockers(got map[string]any) string {
	var b strings.Builder
	for _, p := range got["problems"].([]any) {
		b.WriteString(p.(map[string]any)["msg"].(string))
		b.WriteString("\n")
	}
	return b.String()
}
