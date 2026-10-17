package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type mergedPR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	MergedAt  string `json:"mergedAt"`
	URL       string `json:"url"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
}

func digest(g gh, args []string, r *result) {
	since := "24h"
	for i := 0; i < len(args); i++ {
		if args[i] == "--since" && i+1 < len(args) {
			since = args[i+1]
			i++
			continue
		}
		r.bad("unknown argument "+args[i], "digest --since 24h")
		return
	}
	d, err := time.ParseDuration(since)
	if err != nil || d <= 0 {
		r.bad("--since must be a duration such as 24h", "digest --since 24h")
		return
	}
	from := time.Now().UTC().Add(-d).Format(time.RFC3339)
	r.set("since", from)
	out, errs, code := g.e.sys.exec(cmd{dir: g.e.repo, name: "gh", args: []string{"pr", "list", "--repo", g.repo, "--state", "merged", "--search", "merged:>=" + from, "--json", "number,title,mergedAt,url,additions,deletions,author", "--limit", "200"}})
	if code != 0 {
		r.fail("cannot list merged PRs: "+strings.TrimSpace(errs), "run gh auth status")
		return
	}
	var prs []mergedPR
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		r.fail("cannot read the merged PRs: "+err.Error(), "run gh pr list by hand")
		return
	}
	flagsOf := ledgerFlags(g)
	rows := []map[string]any{}
	var md strings.Builder
	fmt.Fprintf(&md, "## Merged PRs since %s\n\n", from)
	if len(prs) == 0 {
		md.WriteString("No PRs merged.\n")
	}
	flagged := 0
	for _, p := range prs {
		f := flagsOf[strconv.Itoa(p.Number)]
		if f == nil {
			f = []string{}
		}
		if len(f) > 0 {
			flagged++
		}
		rows = append(rows, map[string]any{"number": p.Number, "title": p.Title, "merged_at": p.MergedAt, "url": p.URL, "additions": p.Additions, "deletions": p.Deletions, "author": p.Author.Login, "flags": f})
		fmt.Fprintf(&md, "- #%d %s (+%d -%d, %s)\n", p.Number, p.Title, p.Additions, p.Deletions, p.Author.Login)
		for _, x := range f {
			fmt.Fprintf(&md, "  - flag: %s\n", x)
		}
	}
	fmt.Fprintf(&md, "\n%d merged, %d with flags. Look at the flagged PRs first.\n", len(prs), flagged)
	r.set("prs", rows)
	r.set("markdown", md.String())
}

func ledgerFlags(g gh) map[string][]string {
	flagsOf := map[string][]string{}
	n, err := ledgerIssue(g)
	if err != nil {
		return flagsOf
	}
	var comments []struct {
		Body string `json:"body"`
	}
	if g.api(g.path("issues/"+strconv.Itoa(n)+"/comments"), &comments, "--paginate") != nil {
		return flagsOf
	}
	for _, c := range comments {
		var m struct {
			PR    any      `json:"pr"`
			Flags []string `json:"flags"`
		}
		if json.Unmarshal([]byte(c.Body), &m) == nil {
			flagsOf[fmt.Sprint(m.PR)] = m.Flags
		}
	}
	return flagsOf
}
