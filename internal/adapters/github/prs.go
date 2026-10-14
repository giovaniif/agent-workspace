package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.PRFinder = (*Finder)(nil)

type Finder struct {
	Bin    string
	Remote func(ctx context.Context, dir string) (string, error)

	mu   sync.Mutex
	refs map[string]repoRef
}

type repoRef struct{ owner, name string }

const prsPerRepo = 50

func (f *Finder) PRs(ctx context.Context, dirs []string) (map[string][]domain.PullRequest, error) {
	var refs []repoRef
	var resolved []string
	for _, dir := range dirs {
		if ref, ok := f.resolve(ctx, dir); ok {
			refs = append(refs, ref)
			resolved = append(resolved, dir)
		}
	}
	if len(refs) == 0 {
		return map[string][]domain.PullRequest{}, nil
	}
	query, vars := boardQuery(refs)
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, v := range vars {
		args = append(args, "-f", v)
	}
	bin := f.Bin
	if bin == "" {
		bin = "gh"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	aliases := make([]string, len(resolved))
	for i := range resolved {
		aliases[i] = fmt.Sprintf("r%d", i)
	}
	byAlias, parseErr := parseBoard(stdout.Bytes(), aliases)
	if parseErr == nil && runErr != nil && len(byAlias) == 0 {
		parseErr = errors.New("no repo resolved")
	}
	if parseErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf("gh api graphql: %w: %s", runErr, strings.TrimSpace(stderr.String()))
		}
		return nil, parseErr
	}
	out := map[string][]domain.PullRequest{}
	for i, dir := range resolved {
		if prs, ok := byAlias[aliases[i]]; ok {
			out[dir] = prs
		}
	}
	return out, nil
}

func (f *Finder) resolve(ctx context.Context, dir string) (repoRef, bool) {
	f.mu.Lock()
	ref, ok := f.refs[dir]
	f.mu.Unlock()
	if ok {
		return ref, true
	}
	remote := f.Remote
	if remote == nil {
		remote = originURL
	}
	url, err := remote(ctx, dir)
	if err != nil {
		return repoRef{}, false
	}
	owner, name, ok := parseRemote(url)
	if !ok {
		return repoRef{}, false
	}
	ref = repoRef{owner, name}
	f.mu.Lock()
	if f.refs == nil {
		f.refs = map[string]repoRef{}
	}
	f.refs[dir] = ref
	f.mu.Unlock()
	return ref, true
}

func originURL(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
	return string(out), err
}

func parseRemote(url string) (owner, name string, ok bool) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
		if slash := strings.Index(url, "/"); slash >= 0 {
			url = url[slash+1:]
		} else {
			return "", "", false
		}
	} else if colon := strings.Index(url, ":"); colon >= 0 && strings.Contains(url[:colon], "@") {
		url = url[colon+1:]
	} else {
		return "", "", false
	}
	parts := strings.Split(url, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

const boardFragment = `
    pullRequests(first: %d, orderBy: {field: UPDATED_AT, direction: DESC}, states: [OPEN, MERGED, CLOSED]) {
      nodes {
        number title url headRefName state reviewDecision mergeable author { login }
        reviewThreads(first: 100) { nodes { isResolved comments(first: 1) { nodes { author { __typename } createdAt } } } }
        comments(last: 50) { nodes { author { __typename } createdAt } }
        reviews(last: 50) { nodes { author { __typename } submittedAt } }
        commits(last: 1) { nodes { commit { committedDate statusCheckRollup { contexts(first: 100) { nodes {
          __typename
          ... on CheckRun { name status conclusion detailsUrl }
          ... on StatusContext { context state targetUrl }
        } } } } } }
      }
    }`

func boardQuery(refs []repoRef) (query string, vars []string) {
	var decls, body strings.Builder
	for i, r := range refs {
		if i > 0 {
			decls.WriteString(", ")
		}
		fmt.Fprintf(&decls, "$o%d: String!, $n%d: String!", i, i)
		fmt.Fprintf(&body, "  r%d: repository(owner: $o%d, name: $n%d) {%s\n  }\n", i, i, i, fmt.Sprintf(boardFragment, prsPerRepo))
		vars = append(vars, fmt.Sprintf("o%d=%s", i, r.owner), fmt.Sprintf("n%d=%s", i, r.name))
	}
	return fmt.Sprintf("query(%s) {\n%s}", decls.String(), body.String()), vars
}

type ghActor struct {
	Type string `json:"__typename"`
}

type ghDated struct {
	Author    *ghActor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	Submitted time.Time `json:"submittedAt"`
}

func (d ghDated) comment() domain.PRComment {
	at := d.CreatedAt
	if at.IsZero() {
		at = d.Submitted
	}
	return domain.PRComment{Bot: d.Author != nil && d.Author.Type == "Bot", At: at}
}

type ghNodes[T any] struct {
	Nodes []T `json:"nodes"`
}

type ghPR struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	Head           string `json:"headRefName"`
	State          string `json:"state"`
	ReviewDecision string `json:"reviewDecision"`
	Mergeable      string `json:"mergeable"`
	Author         *struct {
		Login string `json:"login"`
	} `json:"author"`
	Threads ghNodes[struct {
		Resolved bool             `json:"isResolved"`
		Comments ghNodes[ghDated] `json:"comments"`
	}] `json:"reviewThreads"`
	Comments ghNodes[ghDated] `json:"comments"`
	Reviews  ghNodes[ghDated] `json:"reviews"`
	Commits  ghNodes[struct {
		Commit struct {
			CommittedDate time.Time `json:"committedDate"`
			Rollup        *struct {
				Contexts ghNodes[ghCheck] `json:"contexts"`
			} `json:"statusCheckRollup"`
		} `json:"commit"`
	}] `json:"commits"`
}

type ghCheck struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}

func parseBoard(out []byte, aliases []string) (map[string][]domain.PullRequest, error) {
	var resp struct {
		Data   map[string]*struct{ PullRequests ghNodes[ghPR] } `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		if len(resp.Errors) > 0 {
			return nil, errors.New(resp.Errors[0].Message)
		}
		return nil, errors.New("github answered without data")
	}
	byAlias := map[string][]domain.PullRequest{}
	for _, alias := range aliases {
		repo := resp.Data[alias]
		if repo == nil {
			continue
		}
		prs := make([]domain.PullRequest, len(repo.PullRequests.Nodes))
		for i, p := range repo.PullRequests.Nodes {
			prs[i] = p.toDomain()
		}
		byAlias[alias] = prs
	}
	return byAlias, nil
}

func (p ghPR) toDomain() domain.PullRequest {
	pr := domain.PullRequest{
		Number: p.Number, Title: p.Title, URL: p.URL, Head: p.Head, State: domain.PRState(p.State),
		ReviewDecision: domain.ReviewDecision(p.ReviewDecision),
	}
	if p.Author != nil {
		pr.Author = p.Author.Login
	}
	if p.Mergeable == "MERGEABLE" || p.Mergeable == "CONFLICTING" {
		pr.Mergeable = domain.Mergeable(p.Mergeable)
	}

	var pushed time.Time
	var states []domain.CheckState
	if len(p.Commits.Nodes) > 0 {
		commit := p.Commits.Nodes[0].Commit
		pushed = commit.CommittedDate
		if commit.Rollup != nil {
			for _, c := range commit.Rollup.Contexts.Nodes {
				st := c.state()
				states = append(states, st)
				if st == domain.CheckFailing {
					pr.Failing = append(pr.Failing, c.failing())
				}
			}
		}
	}
	pr.Checks = domain.RollupChecks(states)

	var comments []domain.PRComment
	for _, c := range p.Comments.Nodes {
		comments = append(comments, c.comment())
	}
	for _, r := range p.Reviews.Nodes {
		comments = append(comments, r.comment())
	}
	for _, th := range p.Threads.Nodes {
		if !th.Resolved {
			pr.UnresolvedThreads++
		}
		for _, c := range th.Comments.Nodes {
			comments = append(comments, c.comment())
		}
	}
	pr.BotComments = domain.BotCommentsSince(comments, pushed)
	return pr
}

func (c ghCheck) failing() domain.FailingCheck {
	if c.Typename == "StatusContext" {
		return domain.FailingCheck{Name: c.Context, URL: c.TargetURL}
	}
	return domain.FailingCheck{Name: c.Name, URL: c.DetailsURL}
}

func (c ghCheck) state() domain.CheckState {
	if c.Typename == "StatusContext" {
		switch c.State {
		case "SUCCESS":
			return domain.CheckPassing
		case "FAILURE", "ERROR":
			return domain.CheckFailing
		default:
			return domain.CheckPending
		}
	}
	if c.Status != "COMPLETED" {
		return domain.CheckPending
	}
	switch c.Conclusion {
	case "SUCCESS":
		return domain.CheckPassing
	case "NEUTRAL", "SKIPPED":
		return domain.CheckNone
	default:
		return domain.CheckFailing
	}
}
