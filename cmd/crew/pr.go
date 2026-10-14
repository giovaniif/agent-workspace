package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const prUsage = "usage: agentws pr <session id or name> [--json]"

type prCheckJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type prJSON struct {
	Number            int           `json:"number"`
	Title             string        `json:"title"`
	URL               string        `json:"url"`
	Branch            string        `json:"branch"`
	State             string        `json:"state"`
	Checks            string        `json:"checks"`
	FailingChecks     []prCheckJSON `json:"failing_checks"`
	ReviewDecision    string        `json:"review_decision"`
	UnresolvedThreads int           `json:"unresolved_threads"`
	BotComments       int           `json:"bot_comments_since_push"`
	Mergeable         string        `json:"mergeable"`
	ReadyToMerge      bool          `json:"ready_to_merge"`
	Blockers          []string      `json:"blockers"`
}

type prReport struct {
	Session string   `json:"session"`
	Name    string   `json:"name"`
	PRs     []prJSON `json:"prs"`
}

func runPR(args []string, stdout, stderr io.Writer) int {
	session, asJSON, ok := parsePRArgs(args)
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	if err := prCommand(session, asJSON, stdout); err != nil {
		fmt.Fprintf(stderr, "agentws pr: %v\n", err)
		return 1
	}
	return 0
}

func parsePRArgs(args []string) (session string, asJSON, ok bool) {
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-") || session != "":
			return "", false, false
		default:
			session = a
		}
	}
	if session == "" {
		return "", false, false
	}
	return session, asJSON, true
}

func prCommand(session string, asJSON bool, stdout io.Writer) error {
	home, err := rpc.Home()
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := rpc.Connect(ctx, rpc.SocketPath(home), func() error { return spawn(home) })
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		return err
	}
	report, err := prReportFor(sub.State, session)
	if err != nil {
		return err
	}
	if asJSON {
		return writePRJSON(stdout, report)
	}
	writePRText(stdout, report)
	return nil
}

func prReportFor(st rpc.State, arg string) (prReport, error) {
	tasks := map[string]domain.Task{}
	for _, t := range st.Tasks {
		tasks[t.ID] = t
	}
	prsOf := func(s domain.Session) []domain.PullRequest {
		var owned []domain.Worktree
		for _, w := range st.Worktrees {
			if slices.Contains(s.WorktreeIDs, w.ID) {
				owned = append(owned, w)
			}
		}
		return domain.BuildSessionCard(tasks[s.TaskID], s, owned, nil).PRs
	}
	var byName []domain.Session
	var match *domain.Session
	for i, s := range st.Sessions {
		if s.ID == arg {
			match = &st.Sessions[i]
			break
		}
		if domain.NameFor(tasks[s.TaskID], prsOf(s)) == arg {
			byName = append(byName, s)
		}
	}
	if match == nil {
		switch len(byName) {
		case 0:
			return prReport{}, fmt.Errorf("no session %q", arg)
		case 1:
			match = &byName[0]
		default:
			return prReport{}, fmt.Errorf("%q matches %d sessions; use the session id", arg, len(byName))
		}
	}
	prs := prsOf(*match)
	report := prReport{Session: match.ID, Name: domain.NameFor(tasks[match.TaskID], prs), PRs: []prJSON{}}
	for _, pr := range prs {
		report.PRs = append(report.PRs, toPRJSON(pr))
	}
	return report, nil
}

func toPRJSON(pr domain.PullRequest) prJSON {
	out := prJSON{
		Number: pr.Number, Title: pr.Title, URL: pr.URL, Branch: pr.Head, State: string(pr.State),
		Checks: string(pr.Checks), FailingChecks: []prCheckJSON{},
		ReviewDecision: string(pr.ReviewDecision), UnresolvedThreads: pr.UnresolvedThreads,
		BotComments: pr.BotComments, Mergeable: string(pr.Mergeable),
		ReadyToMerge: pr.ReadyToMerge(), Blockers: pr.Blockers(),
	}
	if out.Blockers == nil {
		out.Blockers = []string{}
	}
	for _, f := range pr.Failing {
		out.FailingChecks = append(out.FailingChecks, prCheckJSON{Name: f.Name, URL: f.URL})
	}
	return out
}

func writePRJSON(w io.Writer, r prReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func writePRText(w io.Writer, r prReport) {
	fmt.Fprintf(w, "%s  %s\n", r.Session, r.Name)
	if len(r.PRs) == 0 {
		fmt.Fprintln(w, "no PRs")
		return
	}
	for _, pr := range r.PRs {
		status := "ready to merge"
		switch {
		case pr.State != string(domain.PROpen):
			status = strings.ToLower(pr.State)
		case len(pr.Blockers) > 0:
			status = "blocked: " + strings.Join(pr.Blockers, ", ")
		}
		fmt.Fprintf(w, "\n#%d  %s  %s\n  %s\n", pr.Number, pr.Title, pr.URL, status)
		if len(pr.FailingChecks) > 0 {
			fmt.Fprintln(w, "  failing checks:")
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, f := range pr.FailingChecks {
				fmt.Fprintf(tw, "    %s\t%s\n", f.Name, f.URL)
			}
			_ = tw.Flush()
		}
		if pr.BotComments > 0 {
			fmt.Fprintf(w, "  bot comments since push: %d\n", pr.BotComments)
		}
	}
}
