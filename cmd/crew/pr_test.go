package main

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func prState() rpc.State {
	blocked := &domain.PullRequest{
		Number: 12, Title: "Add login", URL: "https://github.com/o/api/pull/12", Head: "feat", State: domain.PROpen,
		Checks: domain.CheckFailing, ReviewDecision: domain.ReviewChangesRequested, Mergeable: domain.MergeConflicting,
		UnresolvedThreads: 2, BotComments: 4,
		Failing: []domain.FailingCheck{
			{Name: "test", URL: "https://github.com/o/api/actions/runs/1/job/2"},
			{Name: "ci/deploy", URL: "https://ci.example.com/deploy/9"},
		},
	}
	ready := &domain.PullRequest{
		Number: 13, Title: "Docs", URL: "https://github.com/o/web/pull/13", Head: "docs", State: domain.PROpen,
		Checks: domain.CheckPassing, ReviewDecision: domain.ReviewApproved, Mergeable: domain.MergeClean,
	}
	other := &domain.PullRequest{Number: 99, State: domain.PROpen}
	return rpc.State{
		Tasks: []domain.Task{{ID: "t1", Text: "login work"}, {ID: "t2", Text: "other"}},
		Sessions: []domain.Session{
			{ID: "s1", TaskID: "t1", WorktreeIDs: []string{"/w/api-feat", "/w/api-feat2", "/w/web-docs", "/w/api-bare"}},
			{ID: "s12", TaskID: "t2"},
			{ID: "s2", TaskID: "t2", WorktreeIDs: []string{"/w/api-other"}},
		},
		Worktrees: []domain.Worktree{
			{ID: "/w/api-feat", Branch: "feat", PR: blocked},
			{ID: "/w/api-feat2", Branch: "feat", PR: blocked},
			{ID: "/w/web-docs", Branch: "docs", PR: ready},
			{ID: "/w/api-bare", Branch: "bare"},
			{ID: "/w/api-other", Branch: "other", PR: other},
		},
	}
}

func TestPRBoardJSONIsStable(t *testing.T) {
	report, err := prReportFor(prState(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writePRJSON(&out, report); err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/pr.json.golden"
	if *updateGolden {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("json changed; run with -update and review the diff.\ngot:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPRBoardEmptySessionListsNoPRsAsAnEmptyArray(t *testing.T) {
	st := prState()
	st.Sessions = append(st.Sessions, domain.Session{ID: "s3", TaskID: "t2"})
	report, err := prReportFor(st, "s3")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writePRJSON(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"prs": []`) {
		t.Errorf("want an empty array, got:\n%s", out.String())
	}
}

func TestPRBoardFindsSessionByIDThenNameAndRejectsAmbiguity(t *testing.T) {
	st := prState()
	tests := []struct {
		arg     string
		want    string
		wantErr string
	}{
		{"s1", "s1", ""},
		{"s12", "s12", ""},
		{"Add login", "s1", ""},
		{"other", "", `"other" matches 2 sessions`},
		{"nope", "", `no session "nope"`},
	}
	for _, tc := range tests {
		report, err := prReportFor(st, tc.arg)
		switch {
		case tc.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%q: err = %v, want %q", tc.arg, err, tc.wantErr)
			}
		case err != nil:
			t.Errorf("%q: %v", tc.arg, err)
		case report.Session != tc.want:
			t.Errorf("%q: session = %q, want %q", tc.arg, report.Session, tc.want)
		}
	}
}

func TestPRBoardTextShowsBlockersAndLinksFailingChecks(t *testing.T) {
	report, err := prReportFor(prState(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writePRText(&out, report)
	for _, want := range []string{
		"#12", "Add login", "blocked: checks failing, changes requested, merge conflicts, 2 unresolved threads",
		"test       https://github.com/o/api/actions/runs/1/job/2",
		"ci/deploy  https://ci.example.com/deploy/9",
		"bot comments since push: 4",
		"#13", "ready to merge",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "#99") {
		t.Errorf("shows another session's PR:\n%s", out.String())
	}
}

func TestPRBoardParseArgs(t *testing.T) {
	tests := []struct {
		args    []string
		session string
		json    bool
		ok      bool
	}{
		{[]string{"s1"}, "s1", false, true},
		{[]string{"s1", "--json"}, "s1", true, true},
		{[]string{"--json", "s1"}, "s1", true, true},
		{[]string{"--json"}, "", false, false},
		{[]string{}, "", false, false},
		{[]string{"s1", "s2"}, "", false, false},
		{[]string{"s1", "--yaml"}, "", false, false},
	}
	for _, tc := range tests {
		session, asJSON, ok := parsePRArgs(tc.args)
		if session != tc.session || asJSON != tc.json || ok != tc.ok {
			t.Errorf("%v = %q, %v, %v; want %q, %v, %v", tc.args, session, asJSON, ok, tc.session, tc.json, tc.ok)
		}
	}
}
