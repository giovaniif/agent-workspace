package main

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

func mac(e *env, args []string, r *result) {
	if len(args) == 0 || args[0] != "evidence" {
		r.bad("mac needs evidence", "go run ./scripts/verify mac evidence --pr 42")
		return
	}
	o, ok := parse(args[1:], []string{"run", "pr", "out-dir", "repo"}, nil, r)
	if !ok {
		return
	}
	repo := o.get("repo", "giovaniif/crew")
	runID, pr := o.get("run", ""), o.get("pr", "")
	if (runID == "") == (pr == "") {
		r.bad("give --run <id> or --pr <n>", "go run ./scripts/verify mac evidence --pr 42")
		return
	}
	gh := func(a ...string) (string, bool) {
		out, errs, code := e.sys.exec(cmd{dir: e.repo, name: "gh", args: append(a, "--repo", repo)})
		if code != 0 {
			r.fail("gh "+strings.Join(a[:2], " ")+": "+strings.TrimSpace(errs), "run gh auth status")
			return "", false
		}
		return out, true
	}
	if pr != "" {
		out, ok := gh("pr", "view", pr, "--json", "headRefOid")
		if !ok {
			return
		}
		var p struct {
			HeadRefOid string `json:"headRefOid"`
		}
		_ = json.Unmarshal([]byte(out), &p)
		out, ok = gh("run", "list", "--workflow", "ci", "--commit", p.HeadRefOid, "--limit", "1", "--json", "databaseId")
		if !ok {
			return
		}
		var runs []struct {
			DatabaseID int64 `json:"databaseId"`
		}
		_ = json.Unmarshal([]byte(out), &runs)
		if len(runs) == 0 {
			r.fail("no ci run for the head "+p.HeadRefOid+" of PR "+pr, "wait for CI to start, then run the command again")
			return
		}
		runID = strconv.FormatInt(runs[0].DatabaseID, 10)
	}
	r.set("run", runID)
	out, ok := gh("run", "view", runID, "--json", "headSha,conclusion,status,url,jobs")
	if !ok {
		return
	}
	var run struct {
		HeadSha    string `json:"headSha"`
		Conclusion string `json:"conclusion"`
		Status     string `json:"status"`
		URL        string `json:"url"`
		Jobs       []struct {
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			Status     string `json:"status"`
			URL        string `json:"url"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(out), &run); err != nil {
		r.fail("cannot read the run: "+err.Error(), "run gh run view "+runID)
		return
	}
	r.set("head_sha", run.HeadSha)
	r.set("url", run.URL)
	var jobs []map[string]any
	for _, j := range run.Jobs {
		if !strings.Contains(strings.ToLower(j.Name), "macos") {
			continue
		}
		jobs = append(jobs, map[string]any{"name": j.Name, "conclusion": j.Conclusion, "url": j.URL})
		if j.Conclusion != "success" {
			c := j.Conclusion
			if c == "" {
				c = "not finished"
			}
			r.fail("macOS job "+j.Name+" is "+c, "read the job log: gh run view "+runID+" --log-failed")
		}
	}
	if len(jobs) == 0 {
		r.fail("the run has no macOS job", "use a ci run of a PR or of main")
	}
	r.set("jobs", jobs)
	dir, err := outDir(e, o, "mac-"+runID)
	if err != nil {
		r.fail(err.Error(), "check the --out-dir")
		return
	}
	var arts []map[string]any
	if _, errs, code := e.sys.exec(cmd{dir: e.repo, name: "gh", args: []string{"run", "download", runID, "--name", "macos-snapshots", "--dir", dir, "--repo", repo}}); code != 0 {
		_, errs2, code2 := e.sys.exec(cmd{dir: e.repo, name: "gh", args: []string{"run", "download", runID, "--dir", dir, "--repo", repo}})
		if code2 != 0 {
			r.fail("no snapshot artifacts: "+strings.TrimSpace(errs+errs2), "the macos job uploads macos-snapshots; check that it ran")
		}
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if sum, err := fileHash(p); err == nil {
			arts = append(arts, map[string]any{"path": p, "sha256": sum})
		}
		return nil
	})
	if arts == nil {
		arts = []map[string]any{}
	}
	r.set("artifacts", arts)
	r.set("out_dir", dir)
}
