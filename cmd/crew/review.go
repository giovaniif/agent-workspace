package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	reviewUsage  = "usage: crew review <comment|scope|send> ..."
	commentUsage = "usage: crew review comment [--session id] (--file abs-path | --worktree id --path rel-path) --start line [--end line] [--code text] --body text"
	scopeUsage   = "usage: crew review scope [--session id] [--scope last_turn|uncommitted|branch] [--worktree id]"
	sendUsage    = "usage: crew review send [--session id]"
)

var errBadComment = errors.New("bad comment arguments")

func sessionFromEnv() string { return domain.Env(os.Getenv, "SESSION") }

func parseCommentArgs(args []string, stderr io.Writer) (rpc.CommentParams, error) {
	fs := flag.NewFlagSet("review comment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var p rpc.CommentParams
	fs.StringVar(&p.Session, "session", sessionFromEnv(), "session id (default $CREW_SESSION)")
	fs.StringVar(&p.File, "file", "", "absolute path of the file")
	fs.StringVar(&p.Worktree, "worktree", "", "worktree id, with --path")
	fs.StringVar(&p.Path, "path", "", "path in the worktree, with --worktree")
	fs.IntVar(&p.StartLine, "start", 0, "first line")
	fs.IntVar(&p.EndLine, "end", 0, "last line (default: the first)")
	fs.StringVar(&p.Code, "code", "", "the text of the commented lines")
	fs.StringVar(&p.Body, "body", "", "the comment")
	if err := fs.Parse(args); err != nil {
		return p, err
	}
	hasFile := p.File != "" || (p.Worktree != "" && p.Path != "")
	if p.Session == "" || !hasFile || p.StartLine < 1 || p.Body == "" {
		return p, errBadComment
	}
	return p, nil
}

func runReview(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, reviewUsage)
		return 2
	}
	switch args[0] {
	case "comment":
		p, err := parseCommentArgs(args[1:], stderr)
		if err != nil {
			fmt.Fprintln(stderr, commentUsage)
			return 2
		}
		return callReview(stderr, "comment", func(ctx context.Context, c *rpc.Client) error {
			draft, err := c.AddReviewComment(ctx, p)
			if err != nil {
				return err
			}
			if n := len(draft.Comments); n > 0 {
				last := draft.Comments[n-1]
				fmt.Fprintf(stdout, "comment %s on %s:%d (%d in the draft)\n", last.ID, last.Path, last.Start, n)
			}
			return nil
		})
	case "scope":
		p, err := parseScopeArgs(args[1:], stderr)
		if err != nil {
			fmt.Fprintln(stderr, scopeUsage)
			return 2
		}
		return callReview(stderr, "scope", func(ctx context.Context, c *rpc.Client) error {
			rev, err := c.Review(ctx, p)
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(scopeRows(rev))
		})
	case "send":
		fs := flag.NewFlagSet("review send", flag.ContinueOnError)
		fs.SetOutput(stderr)
		session := fs.String("session", sessionFromEnv(), "session id (default $CREW_SESSION)")
		if err := fs.Parse(args[1:]); err != nil || *session == "" || fs.NArg() > 0 {
			fmt.Fprintln(stderr, sendUsage)
			return 2
		}
		return callReview(stderr, "send", func(ctx context.Context, c *rpc.Client) error {
			draft, err := c.SendReview(ctx, *session)
			if err != nil {
				return err
			}
			verb := "queued"
			if draft.Status == domain.DraftSent {
				verb = "sent"
			}
			fmt.Fprintf(stdout, "%s %d comments to %s\n", verb, len(draft.Comments), *session)
			return nil
		})
	default:
		fmt.Fprintln(stderr, reviewUsage)
		return 2
	}
}

func callReview(stderr io.Writer, what string, f func(context.Context, *rpc.Client) error) int {
	ctx := context.Background()
	c, err := connectHome(ctx)
	if err == nil {
		err = f(ctx, c)
		_ = c.Close()
	}
	if err != nil {
		fmt.Fprintf(stderr, "crew review %s: %v\n", what, err)
		return 1
	}
	return 0
}

func parseScopeArgs(args []string, stderr io.Writer) (rpc.ReviewParams, error) {
	fs := flag.NewFlagSet("review scope", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var p rpc.ReviewParams
	scope := fs.String("scope", "", "last_turn, uncommitted or branch (default: the scope the review has open)")
	fs.StringVar(&p.Session, "session", sessionFromEnv(), "session id (default $CREW_SESSION)")
	fs.StringVar(&p.Worktree, "worktree", "", "one worktree id (default: all)")
	if err := fs.Parse(args); err != nil {
		return p, err
	}
	p.Scope = domain.ReviewScope(*scope)
	if p.Session == "" {
		return p, errBadComment
	}
	return p, nil
}

type scopeRow struct {
	Worktree string   `json:"worktree"`
	Path     string   `json:"path"`
	From     string   `json:"from"`
	Files    []string `json:"files"`
	Error    string   `json:"error,omitempty"`
}

func scopeRows(r rpc.Review) []scopeRow {
	rows := make([]scopeRow, 0, len(r.Worktrees))
	for _, w := range r.Worktrees {
		row := scopeRow{Worktree: w.Worktree.ID, Path: w.Worktree.Path, From: w.From, Files: []string{}, Error: w.Err}
		for _, f := range w.Files {
			row.Files = append(row.Files, f.Path)
		}
		rows = append(rows, row)
	}
	return rows
}
