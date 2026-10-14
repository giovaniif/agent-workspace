package domain

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	prNumber = regexp.MustCompile(`^[0-9]+$`)
	notSlug  = regexp.MustCompile(`[^a-z0-9]+`)
)

const (
	slugWords = 5
	slugMax   = 40
)

func ParseWorkItem(input string) Task {
	input = strings.TrimSpace(input)
	text := Task{Source: TaskText, Text: input}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return text
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if strings.TrimPrefix(u.Host, "www.") != "github.com" || len(parts) < 4 || parts[2] != "pull" || !prNumber.MatchString(parts[3]) {
		return text
	}
	return Task{Source: TaskPR, Ref: parts[1] + "#" + parts[3], URL: input}
}

func CheckWorkItem(input string) (Task, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Task{}, errors.New("work item is empty")
	}
	task := ParseWorkItem(input)
	if task.Source == TaskText && (strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")) {
		return Task{}, errors.New("unsupported link: use a GitHub pull request URL")
	}
	return task, nil
}

func TaskSlug(t Task) string {
	src := t.Text
	if t.Source != TaskText {
		src = t.Ref
	}
	words := strings.FieldsFunc(notSlug.ReplaceAllString(strings.ToLower(src), " "), func(r rune) bool { return r == ' ' })
	if len(words) > slugWords {
		words = words[:slugWords]
	}
	slug := strings.Join(words, "-")
	if len(slug) > slugMax {
		slug = strings.TrimRight(slug[:slugMax], "-")
	}
	if slug == "" {
		return "task"
	}
	return slug
}

func FindTask(known []Task, t Task) (Task, bool) {
	for _, k := range known {
		if k.Source != t.Source {
			continue
		}
		if t.Source == TaskText {
			if t.Text != "" && k.Text == t.Text {
				return k, true
			}
		} else if k.Ref == t.Ref {
			return k, true
		}
	}
	return Task{}, false
}

type SessionPlan struct {
	Dir      string
	Worktree *WorktreePlan
}

type WorktreePlan struct {
	Repo     string
	RepoPath string
	Path     string
	Branch   string
	Base     string
}

func PlanSessionStart(ws Workspace, slug, worktreeHome string, taken []string) SessionPlan {
	if ws.Kind != WorkspaceSingle || len(ws.Repos) == 0 {
		return SessionPlan{Dir: ws.Root}
	}
	repo := ws.Repos[0]
	name := slug
	for n := 2; slices.Contains(taken, path.Join(worktreeHome, repo.Name, name)); n++ {
		name = slug + "-" + strconv.Itoa(n)
	}
	base := "HEAD"
	if repo.DefaultBranch != "" {
		base = "origin/" + repo.DefaultBranch
	}
	wt := &WorktreePlan{Repo: repo.Name, RepoPath: repo.Path, Path: path.Join(worktreeHome, repo.Name, name), Branch: name, Base: base}
	return SessionPlan{Dir: wt.Path, Worktree: wt}
}

func (s Session) End() Session {
	s.State = StateIdle
	s.Ended = true
	s.Pane = ""
	s.Focused = false
	return s
}

func (s Session) Forgotten() bool {
	return s.Ended && len(s.WorktreeIDs) == 0
}
