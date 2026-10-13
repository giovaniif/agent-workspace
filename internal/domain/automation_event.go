package domain

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type AutomationFilter struct {
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Author string `json:"author,omitempty"`
}

type AutomationEvent struct {
	Name     string
	Repo     string
	Branch   string
	Author   string
	Subject  string
	WorkItem string
	Text     string
}

func WorktreeAutomationEvents(old *Worktree, wt Worktree) []AutomationEvent {
	var out []AutomationEvent
	if old == nil {
		out = append(out, AutomationEvent{
			Name: "worktree.created", Repo: wt.Repo, Branch: wt.Branch, Subject: wt.Path, WorkItem: wt.Branch,
			Text: fmt.Sprintf("worktree.created: %s at %s", wt.Branch, wt.Path),
		})
	}
	pr := wt.PR
	if pr == nil {
		return out
	}
	var was *PullRequest
	if old != nil && old.PR != nil && old.PR.Number == pr.Number {
		was = old.PR
	}
	add := func(name, detail string) {
		text := fmt.Sprintf("%s: PR #%d %q %s", name, pr.Number, pr.Title, pr.URL)
		if detail != "" {
			text += "\n" + detail
		}
		out = append(out, AutomationEvent{
			Name: name, Repo: wt.Repo, Branch: wt.Branch, Author: pr.Author, Subject: pr.URL, WorkItem: pr.URL, Text: text,
		})
	}
	if was == nil && pr.State == PROpen {
		add("pr.opened", "")
	}
	if pr.Checks == CheckFailing && (was == nil || was.Checks != CheckFailing) {
		var names []string
		for _, f := range pr.Failing {
			names = append(names, f.Name)
		}
		detail := ""
		if len(names) > 0 {
			detail = "failing checks: " + strings.Join(names, ", ")
		}
		add("pr.ci_failed", detail)
	}
	if pr.ReviewDecision == ReviewRequired && (was == nil || was.ReviewDecision != ReviewRequired) {
		add("pr.review_requested", "")
	}
	if pr.State == PRMerged && (was == nil || was.State != PRMerged) {
		add("pr.merged", "")
	}
	return out
}

func SessionAutomationEvents(old, s Session, workItem string) []AutomationEvent {
	name := ""
	switch {
	case s.State == StateDone && old.State != StateDone:
		name = "session.finished"
	case (s.State == StatePermission || s.State == StateWaiting) && old.State != StatePermission && old.State != StateWaiting:
		name = "session.needs_attention"
	default:
		return nil
	}
	return []AutomationEvent{{
		Name: name, Repo: s.Dir, Subject: s.ID, WorkItem: workItem,
		Text: fmt.Sprintf("%s: session %s (%s) is %s in %s", name, s.ID, workItem, s.State, s.Dir),
	}}
}

func MatchAutomationEvent(a Automation, ev AutomationEvent) bool {
	if !a.Enabled || !slices.Contains(a.On, ev.Name) {
		return false
	}
	if filepath.Base(a.Source) == ".agentws.toml" && filepath.Dir(a.Source) != ev.Repo {
		return false
	}
	f := a.Filter
	if f.Repo != "" && f.Repo != ev.Repo && f.Repo != filepath.Base(ev.Repo) {
		return false
	}
	if f.Branch != "" {
		if ok, _ := path.Match(f.Branch, ev.Branch); !ok {
			return false
		}
	}
	return f.Author == "" || f.Author == ev.Author
}
