package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Mate struct {
	Name    string   `json:"name"`
	Prompt  string   `json:"prompt"`
	Agent   string   `json:"agent"`
	Model   string   `json:"model,omitempty"`
	Effort  string   `json:"effort,omitempty"`
	Repos   []string `json:"repos,omitempty"`
	Owner   string   `json:"owner,omitempty"`
	Enabled bool     `json:"enabled"`
}

const mateNameMax = 32

var (
	mateName    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	slackUserID = regexp.MustCompile(`^[UW][A-Z0-9]{8,}$`)
	mateAgents  = []Harness{HarnessClaude, HarnessCodex}
)

func ValidateMate(m Mate, workspaces []Workspace) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("mate %q: %s", m.Name, fmt.Sprintf(format, args...))
	}
	if len(m.Name) > mateNameMax || !mateName.MatchString(m.Name) {
		return fail("name must be 1 to %d characters: lower case letters, digits, - and _, and it must not start with - or _", mateNameMax)
	}
	if strings.TrimSpace(m.Prompt) == "" {
		return fail("prompt is empty")
	}
	h := Harness(m.Agent)
	if !slices.Contains(mateAgents, h) {
		return fail("agent must be one of %v", mateAgents)
	}
	if m.Effort != "" && !slices.Contains(Spec(h).Efforts, m.Effort) {
		return fail("effort %q is not one of %v", m.Effort, Spec(h).Efforts)
	}
	if m.Owner != "" && !slackUserID.MatchString(m.Owner) {
		return fail("owner %q is not a Slack user ID such as U012ABCDEF", m.Owner)
	}
	var seen []Repo
	for _, ref := range m.Repos {
		repo, ok := findMateRepo(workspaces, ref)
		switch {
		case ref == "":
			return fail("repo is empty")
		case !ok:
			return fail("repo %q is not in a registered workspace", ref)
		case slices.Contains(seen, repo):
			return fail("repo %q is listed twice", ref)
		}
		seen = append(seen, repo)
	}
	return nil
}

func findMateRepo(workspaces []Workspace, ref string) (Repo, bool) {
	for _, ws := range workspaces {
		for _, r := range ws.Repos {
			if r.Path == ref || r.Name == ref {
				return Repo{Name: r.Name, Path: r.Path}, true
			}
		}
	}
	return Repo{}, false
}

func CheckMates(mates []Mate, workspaces []Workspace) ([]Mate, []error) {
	var valid []Mate
	var problems []error
	seen := map[string]bool{}
	for _, m := range mates {
		if err := ValidateMate(m, workspaces); err != nil {
			problems = append(problems, err)
			continue
		}
		if seen[m.Name] {
			problems = append(problems, fmt.Errorf("mate %q is defined twice", m.Name))
			continue
		}
		seen[m.Name] = true
		valid = append(valid, m)
	}
	return valid, problems
}
