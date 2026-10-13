package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

func TestPRBoardParseBoardReadsEverythingTheBoardShows(t *testing.T) {
	out, err := os.ReadFile("testdata/board.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseBoard(out, []string{"r0", "r1"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]domain.PullRequest{
		"r0": {
			{
				Number: 12, Title: "Add login", URL: "https://github.com/o/api/pull/12", Head: "feat", Author: "ana", State: domain.PROpen,
				Checks: domain.CheckFailing, ReviewDecision: domain.ReviewChangesRequested, Mergeable: domain.MergeConflicting,
				UnresolvedThreads: 2, BotComments: 4,
				Failing: []domain.FailingCheck{
					{Name: "test", URL: "https://github.com/o/api/actions/runs/1/job/2"},
					{Name: "ci/deploy", URL: "https://ci.example.com/deploy/9"},
				},
			},
			{Number: 11, Title: "Fix", URL: "u11", Head: "fix", State: domain.PRMerged},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestPRBoardParseBoardRejectsGarbageAndEmptyData(t *testing.T) {
	for _, in := range []string{"not json", `{"errors":[{"message":"Bad credentials"}]}`, `{"data":null}`} {
		if _, err := parseBoard([]byte(in), []string{"r0"}); err == nil {
			t.Errorf("parseBoard(%q): want an error", in)
		}
	}
}

func TestPRBoardCheckStates(t *testing.T) {
	tests := []struct {
		check ghCheck
		want  domain.CheckState
	}{
		{ghCheck{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SUCCESS"}, domain.CheckPassing},
		{ghCheck{Typename: "CheckRun", Status: "IN_PROGRESS"}, domain.CheckPending},
		{ghCheck{Typename: "CheckRun", Status: "QUEUED"}, domain.CheckPending},
		{ghCheck{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SKIPPED"}, domain.CheckNone},
		{ghCheck{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "NEUTRAL"}, domain.CheckNone},
		{ghCheck{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "TIMED_OUT"}, domain.CheckFailing},
		{ghCheck{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "CANCELLED"}, domain.CheckFailing},
		{ghCheck{Typename: "StatusContext", State: "SUCCESS"}, domain.CheckPassing},
		{ghCheck{Typename: "StatusContext", State: "PENDING"}, domain.CheckPending},
		{ghCheck{Typename: "StatusContext", State: "EXPECTED"}, domain.CheckPending},
		{ghCheck{Typename: "StatusContext", State: "FAILURE"}, domain.CheckFailing},
		{ghCheck{Typename: "StatusContext", State: "ERROR"}, domain.CheckFailing},
	}
	for _, tc := range tests {
		if got := tc.check.state(); got != tc.want {
			t.Errorf("%+v = %q, want %q", tc.check, got, tc.want)
		}
	}
}

func TestPRBoardParseRemote(t *testing.T) {
	tests := []struct {
		url         string
		owner, name string
		ok          bool
	}{
		{"https://github.com/o/api.git", "o", "api", true},
		{"https://github.com/o/api", "o", "api", true},
		{"https://github.com/o/api/", "o", "api", true},
		{"git@github.com:o/api.git", "o", "api", true},
		{"ssh://git@github.com/o/api.git", "o", "api", true},
		{"https://ghe.example.com/team/web.git\n", "team", "web", true},
		{"/local/path/api", "", "", false},
		{"", "", "", false},
		{"https://github.com/api", "", "", false},
	}
	for _, tc := range tests {
		owner, name, ok := parseRemote(tc.url)
		if owner != tc.owner || name != tc.name || ok != tc.ok {
			t.Errorf("parseRemote(%q) = %q, %q, %v; want %q, %q, %v", tc.url, owner, name, ok, tc.owner, tc.name, tc.ok)
		}
	}
}

func TestPRBoardQueryIsReadOnlyAndNamesEveryRepo(t *testing.T) {
	query, vars := boardQuery([]repoRef{{"o", "api"}, {"o", "web"}, {"p", "docs"}})
	if strings.Contains(strings.ToLower(query), "mutation") {
		t.Error("the board query must never mutate")
	}
	for _, alias := range []string{"r0:", "r1:", "r2:"} {
		if !strings.Contains(query, alias) {
			t.Errorf("query has no %s", alias)
		}
	}
	want := []string{"o0=o", "n0=api", "o1=o", "n1=web", "o2=p", "n2=docs"}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("vars = %v, want %v", vars, want)
	}
}

func fakeGH(t *testing.T, canned string, exit int) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	cannedPath := filepath.Join(dir, "canned.json")
	if err := os.WriteFile(cannedPath, []byte(canned), 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\necho \"$1 $2\" >> '%s'\ncat '%s'\necho boom >&2\nexit %d\n", log, cannedPath, exit)
	bin = filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func remotes(m map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, dir string) (string, error) {
		if u, ok := m[dir]; ok {
			return u, nil
		}
		return "", errors.New("no origin")
	}
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestPRBoardFinderMakesOneRequestForAllRepos(t *testing.T) {
	canned, err := os.ReadFile("testdata/board.json")
	if err != nil {
		t.Fatal(err)
	}
	bin, log := fakeGH(t, string(canned), 0)
	repos := map[string]string{}
	var dirs []string
	for i := range 6 {
		dir := fmt.Sprintf("/w/repo%d", i)
		repos[dir] = fmt.Sprintf("git@github.com:o/repo%d.git", i)
		dirs = append(dirs, dir)
	}
	f := &Finder{Bin: bin, Remote: remotes(repos)}
	got, err := f.PRs(context.Background(), dirs)
	if err != nil {
		t.Fatal(err)
	}
	if c := calls(t, log); len(c) != 1 || !strings.HasPrefix(c[0], "api graphql") {
		t.Errorf("gh calls = %q, want exactly one `gh api graphql`", c)
	}
	if len(got["/w/repo0"]) != 2 || got["/w/repo0"][0].Number != 12 {
		t.Errorf("repo0 PRs = %+v", got["/w/repo0"])
	}
	if _, ok := got["/w/repo1"]; ok {
		t.Error("a repo GitHub could not resolve must be left out")
	}
}

func TestPRBoardFinderSkipsReposWithoutARemoteAndCachesTheOnesItFound(t *testing.T) {
	bin, log := fakeGH(t, `{"data":{"r0":{"pullRequests":{"nodes":[]}}}}`, 0)
	resolved := 0
	f := &Finder{Bin: bin, Remote: func(ctx context.Context, dir string) (string, error) {
		resolved++
		return remotes(map[string]string{"/w/api": "https://github.com/o/api.git"})(ctx, dir)
	}}
	for range 3 {
		got, err := f.PRs(context.Background(), []string{"/w/api", "/w/local"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["/w/local"]; ok {
			t.Error("a repo with no remote has no PRs")
		}
	}
	if resolved != 4 {
		t.Errorf("resolved %d remotes over 3 polls, want 4 (api once, local every poll)", resolved)
	}
	if n := len(calls(t, log)); n != 3 {
		t.Errorf("%d requests over 3 polls, want 3", n)
	}
}

func TestPRBoardFinderAsksGitHubNothingWhenNoRepoHasARemote(t *testing.T) {
	bin, log := fakeGH(t, `{}`, 0)
	f := &Finder{Bin: bin, Remote: remotes(nil)}
	got, err := f.PRs(context.Background(), []string{"/w/local"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if c := calls(t, log); len(c) != 0 {
		t.Errorf("gh ran: %q", c)
	}
}

func TestPRBoardFinderKeepsWhatResolvedWhenGHExitsNonZeroOnAPartialError(t *testing.T) {
	canned, _ := os.ReadFile("testdata/board.json")
	bin, _ := fakeGH(t, string(canned), 1)
	f := &Finder{Bin: bin, Remote: remotes(map[string]string{"/w/api": "https://github.com/o/api", "/w/gone": "https://github.com/o/gone"})}
	got, err := f.PRs(context.Background(), []string{"/w/api", "/w/gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["/w/api"]) != 2 {
		t.Errorf("api PRs = %+v", got["/w/api"])
	}
}

func TestPRBoardFinderReportsAFailedRequest(t *testing.T) {
	bin, _ := fakeGH(t, ``, 1)
	f := &Finder{Bin: bin, Remote: remotes(map[string]string{"/w/api": "https://github.com/o/api"})}
	if _, err := f.PRs(context.Background(), []string{"/w/api"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want it to carry gh's stderr", err)
	}
}

func TestPRBoardFinderFailsThePollWhenNoRepoResolvedAndGHFailed(t *testing.T) {
	bin, _ := fakeGH(t, `{"data":{"r0":null,"r1":null},"errors":[{"message":"Bad credentials"}]}`, 1)
	f := &Finder{Bin: bin, Remote: remotes(map[string]string{"/w/a": "https://github.com/o/a", "/w/b": "https://github.com/o/b"})}
	if _, err := f.PRs(context.Background(), []string{"/w/a", "/w/b"}); err == nil {
		t.Error("want an error so the daemon backs off")
	}
}

func TestPRBoardFinderKeepsAnEmptyRepoAsASuccess(t *testing.T) {
	bin, _ := fakeGH(t, `{"data":{"r0":{"pullRequests":{"nodes":[]}}}}`, 0)
	f := &Finder{Bin: bin, Remote: remotes(map[string]string{"/w/a": "https://github.com/o/a"})}
	got, err := f.PRs(context.Background(), []string{"/w/a"})
	if err != nil {
		t.Fatal(err)
	}
	if prs, ok := got["/w/a"]; !ok || len(prs) != 0 {
		t.Errorf("got %v, want an empty answer for the repo", got)
	}
}
