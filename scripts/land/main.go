package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type cmd struct {
	dir  string
	name string
	args []string
}

type sys interface {
	exec(c cmd) (string, string, int)
}

type realSys struct{}

func (realSys) exec(c cmd) (string, string, int) {
	x := exec.Command(c.name, c.args...)
	x.Dir = c.dir
	var out, errb bytes.Buffer
	x.Stdout, x.Stderr = &out, &errb
	err := x.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &ee):
		return out.String(), errb.String(), ee.ExitCode()
	}
	return out.String(), err.Error(), 127
}

type env struct {
	out  io.Writer
	sys  sys
	repo string
}

type problem struct {
	Msg  string `json:"msg"`
	Hint string `json:"hint"`
}

type result struct {
	data     map[string]any
	problems []problem
	flags    []string
	strict   bool
	usage    bool
}

func (r *result) flag(msg, hint string) {
	if r.strict {
		r.fail(msg, hint)
		return
	}
	r.flags = append(r.flags, msg)
}

func (r *result) fail(msg, hint string) { r.problems = append(r.problems, problem{msg, hint}) }
func (r *result) bad(msg, hint string)  { r.usage = true; r.fail(msg, hint) }
func (r *result) set(k string, v any)   { r.data[k] = v }

type config struct {
	Mode string `json:"mode"`
	Repo string `json:"repo"`
}

const usage = "go run ./scripts/land check|verdict|merge|watch|ledger <pr>, or digest --since 24h"

func run(args []string, e *env) int {
	r := &result{data: map[string]any{}}
	defer emit(e.out, r)
	if len(args) >= 1 && args[0] == "digest" {
		digest(gh{e: e, repo: loadConfig(e).Repo}, args[1:], r)
		return code(r)
	}
	if len(args) < 2 && (len(args) != 1 || args[0] != "ledger") {
		r.bad("land needs a command and a PR number", usage)
		return code(r)
	}
	cfg := loadConfig(e)
	r.set("mode", cfg.Mode)
	pr := ""
	if len(args) > 1 {
		pr = args[1]
		if _, err := strconv.Atoi(pr); err != nil {
			r.bad("not a PR number: "+pr, usage)
			return code(r)
		}
		r.set("pr", pr)
	}
	g := gh{e: e, repo: cfg.Repo}
	if args[0] == "check" || args[0] == "merge" {
		for _, a := range args[2:] {
			if a != "--strict" {
				r.bad("unknown argument "+a, usage)
				return code(r)
			}
			r.strict = true
		}
	}
	switch args[0] {
	case "check":
		check(e, g, pr, r)
	case "verdict":
		verdict(e, g, pr, args[2:], r)
	case "merge":
		merge(e, g, cfg, pr, r)
	case "watch":
		watch(g, pr, r)
	case "ledger":
		ledgerList(g, pr, r)
	default:
		r.bad("unknown command "+args[0], usage)
	}
	return code(r)
}

func code(r *result) int {
	switch {
	case r.usage:
		return 2
	case len(r.problems) > 0:
		return 1
	}
	return 0
}

func emit(w io.Writer, r *result) {
	out := map[string]any{}
	for k, v := range r.data {
		out[k] = v
	}
	if r.problems == nil {
		r.problems = []problem{}
	}
	if r.flags == nil {
		r.flags = []string{}
	}
	out["flags"] = r.flags
	out["ok"] = len(r.problems) == 0
	out["problems"] = r.problems
	b, _ := json.MarshalIndent(out, "", "  ")
	b = append(b, '\n')
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return
		}
		b = b[n:]
	}
}

func loadConfig(e *env) config {
	c := config{Mode: "live", Repo: "giovaniif/crew"}
	if b, err := os.ReadFile(filepath.Join(e.repo, "scripts", "land", "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Mode != "off" && c.Mode != "shadow" && c.Mode != "live" {
		c.Mode = "live"
	}
	return c
}

type gh struct {
	e    *env
	repo string
}

func (g gh) api(path string, v any, extra ...string) error {
	out, errs, code := g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: append([]string{"api", path}, extra...)})
	if code != 0 {
		return fmt.Errorf("gh api %s: %s", path, strings.TrimSpace(errs))
	}
	return json.Unmarshal([]byte(out), v)
}

func (g gh) path(p string) string { return "repos/" + g.repo + "/" + p }

type pull struct {
	Number         int    `json:"number"`
	Draft          bool   `json:"draft"`
	MergeableState string `json:"mergeable_state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type file struct {
	Filename string  `json:"filename"`
	Status   string  `json:"status"`
	Patch    *string `json:"patch"`
	Previous string  `json:"previous_filename"`
}

var verdictMarker = regexp.MustCompile(`<!-- land:verdict v=1 sha=([0-9a-f]+) verdict=([A-Z+]+) -->`)

func check(e *env, g gh, pr string, r *result) bool {
	var p pull
	if err := g.api(g.path("pulls/"+pr), &p); err != nil {
		r.fail("cannot read the PR: "+err.Error(), "run gh auth status")
		r.set("landable", false)
		return false
	}
	head := p.Head.SHA
	r.set("head", head)
	if p.Draft {
		r.fail("the PR is a draft", "mark it ready for review")
	}
	if p.MergeableState != "clean" {
		r.fail("mergeable state is "+p.MergeableState+", not clean", "update the branch or resolve what blocks it")
	}
	checkVerdict(g, pr, head, r)
	checkChecks(g, p, r)
	checkThreads(g, pr, r)
	checkFiles(g, pr, r)
	if _, err := os.Stat(filepath.Join(e.repo, ".claude", "skills", "verify", "SKILL.md")); err != nil {
		r.flag("no verification skill in this repo", "add .claude/skills/verify/SKILL.md")
	}
	ok := len(r.problems) == 0
	r.set("landable", ok)
	return ok
}

func checkVerdict(g gh, pr, head string, r *result) {
	var comments []struct {
		Body string `json:"body"`
	}
	if err := g.api(g.path("issues/"+pr+"/comments"), &comments, "--paginate"); err != nil {
		r.fail("cannot read the verdict: "+err.Error(), "run gh auth status")
		return
	}
	sha, v := "", ""
	for _, c := range comments {
		if m := verdictMarker.FindStringSubmatch(c.Body); m != nil {
			sha, v = m[1], m[2]
		}
	}
	r.set("verdict", v)
	switch {
	case v == "":
		r.flag("no verifier verdict", "a fresh agent runs the verifier pass and go run ./scripts/land verdict")
	case sha != head:
		r.flag("the verdict is for an older head "+sha, "run the verifier pass again on "+head)
	case v != "PASS" && v != "PASS+NOTES":
		r.flag("the verdict is "+v, "a human decides")
	}
}

func checkChecks(g gh, p pull, r *result) {
	var prot struct {
		Required struct {
			Contexts []string `json:"contexts"`
		} `json:"required_status_checks"`
	}
	known := true
	if err := g.api(g.path("branches/"+p.Base.Ref+"/protection"), &prot); err != nil || len(prot.Required.Contexts) == 0 {
		known = false
		r.fail("cannot read the required checks of "+p.Base.Ref, "check the token can read branch protection; every check now blocks")
	}
	state := map[string]string{}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := g.api(g.path("commits/"+p.Head.SHA+"/check-runs"), &runs, "--paginate"); err != nil {
		r.fail("cannot read the check runs: "+err.Error(), "run gh auth status")
	}
	for _, c := range runs.CheckRuns {
		s := c.Conclusion
		if c.Status != "completed" {
			s = c.Status
		}
		state[c.Name] = s
	}
	var st struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	if err := g.api(g.path("commits/"+p.Head.SHA+"/status"), &st); err == nil {
		for _, s := range st.Statuses {
			if _, ok := state[s.Context]; !ok {
				state[s.Context] = s.State
			}
		}
	}
	good := func(s string) bool { return s == "success" || s == "skipped" || s == "neutral" }
	if known {
		for _, c := range prot.Required.Contexts {
			if s, ok := state[c]; !ok {
				r.fail("required check "+c+" has not reported", "wait for CI, or request the review (@coderabbitai review)")
			} else if !good(s) {
				r.fail("required check "+c+" is "+s, "wait for it to finish, or fix the failure; do not re-run to get a pass")
			}
		}
	} else {
		for c, s := range state {
			if !good(s) {
				r.fail("check "+c+" is "+s, "fix the failure")
			}
		}
	}
	r.set("checks", state)
}

func checkThreads(g gh, pr string, r *result) {
	owner, name, _ := strings.Cut(g.repo, "/")
	q := `query($o:String!,$n:String!,$p:Int!){repository(owner:$o,name:$n){pullRequest(number:$p){reviewDecision reviewThreads(first:100){pageInfo{hasNextPage} nodes{isResolved}}}}}`
	out, _, code := g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: []string{"api", "graphql", "-f", "query=" + q, "-f", "o=" + owner, "-f", "n=" + name, "-F", "p=" + pr}})
	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewDecision string `json:"reviewDecision"`
					ReviewThreads  struct {
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &resp) != nil {
		r.fail("unknown review thread state", "run gh auth status; an unknown state blocks")
		return
	}
	p := resp.Data.Repository.PullRequest
	if p.ReviewThreads.PageInfo.HasNextPage {
		r.fail("more than 100 review threads; thread state unknown", "a human decides")
	}
	open := 0
	for _, t := range p.ReviewThreads.Nodes {
		if !t.IsResolved {
			open++
		}
	}
	if open > 0 {
		r.fail(fmt.Sprintf("%d unresolved review thread(s)", open), "fix or answer each finding and resolve the thread")
	}
	if p.ReviewDecision == "CHANGES_REQUESTED" {
		r.fail("a reviewer has changes requested", "address the review")
	}
}

var selfOrCI = []string{".github/", ".coderabbit", "scripts/land/", "scripts/verify/", ".claude/skills/", "scripts/tdd-check", "scripts/mutate", "scripts/lint-", ".golangci.yml", "CODEOWNERS"}

var irreversiblePaths = []string{".goreleaser.yaml", "scripts/install.sh", "internal/adapters/onboard/"}

var irreversiblePrefixes = []string{"cmd/crew/setup", "internal/adapters/claude/setup", "internal/adapters/codex/setup", "internal/adapters/omp/setup", "internal/app/cleanup", "internal/domain/cleanup"}

var destructiveSQL = regexp.MustCompile(`(?i)\b(drop\s+(table|index|view|trigger|column)|truncate\b|delete\s+from\b|update\s+\S+\s+set\b|alter\s+table\s+\S+\s+(rename|drop|alter))`)

func isTest(f string) bool {
	return strings.HasSuffix(f, "_test.go") || strings.Contains(f, "/testdata/")
}

func addedText(patch string) string {
	var b strings.Builder
	for _, l := range strings.Split(patch, "\n") {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			b.WriteString(l[1:])
			b.WriteString(" ")
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func checkFiles(g gh, pr string, r *result) {
	var files []file
	if err := g.api(g.path("pulls/"+pr+"/files"), &files, "--paginate"); err != nil {
		r.fail("cannot read the diff: "+err.Error(), "run gh auth status; an unread diff blocks")
		return
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Filename)
		paths := []string{f.Filename}
		if f.Previous != "" {
			paths = append(paths, f.Previous)
		}
		for _, p := range paths {
			for _, s := range selfOrCI {
				if strings.HasPrefix(p, s) || strings.Contains(p, "/"+s) {
					r.flag("self-or-CI change: "+p, "a human merges changes to CI, the gate and the skills")
					break
				}
			}
			if isTest(p) {
				continue
			}
			base := filepath.Base(p)
			hit := false
			for _, s := range irreversiblePaths {
				hit = hit || p == s || strings.HasPrefix(p, s)
			}
			for _, s := range irreversiblePrefixes {
				hit = hit || strings.HasPrefix(p, s)
			}
			hit = hit || p == ".github/workflows/release.yml" || base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
			if strings.HasPrefix(p, "internal/adapters/sqlite/migrations/") && (f.Status == "removed" || f.Status == "renamed") {
				hit = true
			}
			if hit {
				r.flag("irreversible effect: "+p, "a revert cannot undo it; a human decides")
			}
		}
		if f.Patch == nil {
			r.flag("no patch for "+f.Filename+" (fail closed)", "the diff was not scanned; a human decides")
			continue
		}
		added := addedText(*f.Patch)
		if strings.HasPrefix(f.Filename, "internal/adapters/sqlite/migrations/") && destructiveSQL.MatchString(added) {
			r.flag("irreversible effect: destructive SQL in "+f.Filename+": "+destructiveSQL.FindString(added), "a revert cannot undo a migration; a human decides")
		}
		if strings.HasPrefix(f.Filename, "internal/adapters/git/") && !isTest(f.Filename) && strings.Contains(added, `"worktree", "remove"`) {
			r.flag("irreversible effect: worktree removal in "+f.Filename, "cleanup code needs a human")
		}
	}
	r.set("files", names)
}

func verdict(e *env, g gh, pr string, args []string, r *result) {
	v, bundle, notes, dry := "", "", "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verdict", "--bundle", "--notes":
			if i+1 >= len(args) {
				r.bad(args[i]+" needs a value", usage)
				return
			}
			switch args[i] {
			case "--verdict":
				v = args[i+1]
			case "--bundle":
				bundle = args[i+1]
			default:
				notes = args[i+1]
			}
			i++
		case "--dry-run":
			dry = true
		default:
			r.bad("unknown argument "+args[i], "verdict <pr> --verdict PASS|PASS+NOTES|FAIL|HUMAN [--bundle dir] [--notes text] [--dry-run]")
			return
		}
	}
	if v != "PASS" && v != "PASS+NOTES" && v != "FAIL" && v != "HUMAN" {
		r.bad("--verdict must be PASS, PASS+NOTES, FAIL or HUMAN", "--verdict PASS")
		return
	}
	var p pull
	if err := g.api(g.path("pulls/"+pr), &p); err != nil {
		r.fail("cannot read the PR: "+err.Error(), "run gh auth status")
		return
	}
	head := p.Head.SHA
	r.set("head", head)
	if strings.HasPrefix(v, "PASS") {
		if bundle == "" {
			r.fail("PASS needs --bundle", "give the evidence bundle dir")
			return
		}
		out, _, code := e.sys.exec(cmd{dir: e.repo, name: "go", args: []string{"run", "./scripts/verify", "evidence", "verify", bundle}})
		var res struct {
			OK       bool      `json:"ok"`
			Head     string    `json:"head"`
			Problems []problem `json:"problems"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || !res.OK {
			for _, pb := range res.Problems {
				r.fail("bundle: "+pb.Msg, pb.Hint)
			}
			r.fail("the evidence bundle does not verify", "go run ./scripts/verify evidence verify "+bundle)
			return
		}
		if res.Head != head {
			r.fail("the bundle is for head "+res.Head+", not the PR head "+head, "make a bundle for the current head")
			return
		}
	}
	body := "**Verifier verdict: " + v + "**\n"
	if notes != "" {
		body += "\n" + notes + "\n"
	}
	body += "\n<!-- land:verdict v=1 sha=" + head + " verdict=" + v + " -->"
	r.set("comment", body)
	r.set("verdict", v)
	if dry {
		r.set("posted", false)
		return
	}
	if _, errs, code := e.sys.exec(cmd{dir: e.repo, name: "gh", args: []string{"api", "-X", "POST", g.path("issues/" + pr + "/comments"), "-f", "body=" + body}}); code != 0 {
		r.fail("cannot post the verdict: "+strings.TrimSpace(errs), "run gh auth status")
		return
	}
	r.set("posted", true)
}

func merge(e *env, g gh, cfg config, pr string, r *result) {
	if cfg.Mode == "off" {
		r.set("action", "off")
		return
	}
	ok := check(e, g, pr, r)
	head, _ := r.data["head"].(string)
	action := "blocked"
	switch {
	case ok && cfg.Mode == "shadow":
		action = "would-land"
	case ok && cfg.Mode == "live":
		action = "landed"
		if _, errs, code := e.sys.exec(cmd{dir: e.repo, name: "gh", args: []string{"pr", "merge", pr, "--merge", "--match-head-commit", head, "--repo", g.repo}}); code != 0 {
			action = "merge-failed"
			r.fail("gh pr merge failed: "+strings.TrimSpace(errs), "the head may have moved; run check again")
		}
	}
	r.set("action", action)
	var reasons []string
	for _, p := range r.problems {
		reasons = append(reasons, p.Msg)
	}
	entry := map[string]any{"pr": pr, "head": head, "mode": cfg.Mode, "action": action, "blockers": reasons, "flags": r.flags, "at": time.Now().UTC().Format(time.RFC3339)}
	if err := ledgerWrite(g, entry); err != nil {
		r.fail("cannot write the ledger: "+err.Error(), "check the land-ledger issue")
	}
}

func watch(g gh, pr string, r *result) {
	var p pull
	if err := g.api(g.path("pulls/"+pr), &p); err != nil {
		r.fail("cannot read the PR: "+err.Error(), "run gh auth status")
		return
	}
	if !p.Merged {
		r.fail("the PR is not merged", "watch runs after a merge")
		return
	}
	r.set("merge_commit", p.MergeCommitSHA)
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			URL        string `json:"html_url"`
		} `json:"check_runs"`
	}
	if err := g.api(g.path("commits/"+p.MergeCommitSHA+"/check-runs"), &runs, "--paginate"); err != nil {
		r.fail("cannot read the checks of the merge commit: "+err.Error(), "run gh auth status")
		return
	}
	pending, regression := 0, false
	for _, c := range runs.CheckRuns {
		switch {
		case c.Status != "completed":
			pending++
		case c.Conclusion == "failure" || c.Conclusion == "timed_out" || c.Conclusion == "cancelled":
			regression = true
			r.fail("check "+c.Name+" is "+c.Conclusion+" on the merge commit "+c.URL, "open a revert PR (git revert -m 1 "+p.MergeCommitSHA+") and ping the owner; a revert never lands through the gate")
		}
	}
	r.set("pending", pending)
	r.set("regression", regression)
}

func ledgerIssue(g gh) (int, error) {
	out, errs, code := g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: []string{"issue", "list", "--repo", g.repo, "--label", "land-ledger", "--state", "open", "--json", "number", "--limit", "1"}})
	if code != 0 {
		return 0, errors.New(strings.TrimSpace(errs))
	}
	var issues []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return 0, err
	}
	if len(issues) > 0 {
		return issues[0].Number, nil
	}
	out, errs, code = g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: []string{"issue", "create", "--repo", g.repo, "--label", "land-ledger", "--title", "land ledger", "--body", "Each comment is one decision of scripts/land, as JSON. Do not edit."}})
	if code != 0 {
		return 0, errors.New(strings.TrimSpace(errs))
	}
	return strconv.Atoi(filepath.Base(strings.TrimSpace(out)))
}

func ledgerWrite(g gh, entry map[string]any) error {
	n, err := ledgerIssue(g)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(entry)
	_, errs, code := g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: []string{"api", "-X", "POST", g.path("issues/" + strconv.Itoa(n) + "/comments"), "-f", "body=" + string(b)}})
	if code != 0 {
		return errors.New(strings.TrimSpace(errs))
	}
	return nil
}

func ledgerList(g gh, pr string, r *result) {
	n, err := ledgerIssue(g)
	if err != nil {
		r.fail("cannot find the ledger: "+err.Error(), "run gh auth status")
		return
	}
	var comments []struct {
		Body string `json:"body"`
	}
	if err := g.api(g.path("issues/"+strconv.Itoa(n)+"/comments"), &comments, "--paginate"); err != nil {
		r.fail("cannot read the ledger: "+err.Error(), "run gh auth status")
		return
	}
	entries := []map[string]any{}
	for _, c := range comments {
		var m map[string]any
		if json.Unmarshal([]byte(c.Body), &m) != nil {
			continue
		}
		if pr != "" && fmt.Sprint(m["pr"]) != pr {
			continue
		}
		entries = append(entries, m)
	}
	r.set("ledger_issue", n)
	r.set("entries", entries)
}

func main() {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			dir = d
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	os.Exit(run(os.Args[1:], &env{out: os.Stdout, sys: realSys{}, repo: dir}))
}
