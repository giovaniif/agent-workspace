package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type ReviewComment struct {
	ID       string   `json:"id,omitempty"`
	Worktree string   `json:"worktree"`
	Path     string   `json:"path"`
	Start    int      `json:"start"`
	End      int      `json:"end"`
	Removed  bool     `json:"removed,omitempty"`
	Code     []string `json:"code"`
	Body     string   `json:"body"`
}

func CommentOn(worktree, path string, lines []DiffLine, body string) ReviewComment {
	c := ReviewComment{Worktree: worktree, Path: path, Body: strings.TrimSpace(body), Removed: true}
	for _, l := range lines {
		if l.Kind != LineDeleted {
			c.Removed = false
		}
	}
	for _, l := range lines {
		c.Code = append(c.Code, l.Text)
		n := l.New
		if c.Removed {
			n = l.Old
		}
		if n == 0 {
			continue
		}
		if c.Start == 0 || n < c.Start {
			c.Start = n
		}
		c.End = max(c.End, n)
	}
	return c
}

const promptLead = "Review comments on your changes. Each one names the worktree, file and lines it is about; make the edit in that worktree.\n"

func ReviewPrompt(comments []ReviewComment) string {
	var b strings.Builder
	b.WriteString(promptLead)
	for i, c := range comments {
		b.WriteString("\n" + strconv.Itoa(i+1) + ". " + c.Worktree + ":" + c.Path + ":" + lineRange(c.Start, c.End))
		if c.Removed {
			b.WriteString(" (removed lines)")
		}
		b.WriteString("\n")
		for _, l := range dedent(c.Code) {
			if l == "" {
				b.WriteString("   >\n")
				continue
			}
			b.WriteString("   > " + l + "\n")
		}
		for l := range strings.Lines(c.Body) {
			b.WriteString("   " + strings.TrimRight(l, "\n") + "\n")
		}
	}
	return b.String()
}

func lineRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

func dedent(lines []string) []string {
	common := ""
	first := true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			common, first = ind, false
			continue
		}
		for !strings.HasPrefix(ind, common) {
			common = common[:len(common)-1]
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out[i] = strings.TrimPrefix(l, common)
	}
	return out
}

type DraftStatus string

const (
	DraftOpen   DraftStatus = "open"
	DraftQueued DraftStatus = "queued"
	DraftSent   DraftStatus = "sent"
	DraftMerged DraftStatus = "merged"
)

type ReviewDraft struct {
	ID       string          `json:"id"`
	Session  string          `json:"session"`
	Status   DraftStatus     `json:"status"`
	Comments []ReviewComment `json:"comments"`
	SentAt   time.Time       `json:"sent_at,omitzero"`
	Turns    []string        `json:"turns"`
	Note     string          `json:"note,omitempty"`
}

func (d ReviewDraft) WithNote(note string) ReviewDraft {
	d.Note = strings.TrimSpace(note)
	return d
}

func (d ReviewDraft) prompt() string {
	out := ReviewPrompt(d.Comments)
	if d.Note == "" {
		return out
	}
	out += "\nOverall:\n"
	for l := range strings.Lines(d.Note) {
		out += "   " + strings.TrimRight(l, "\n") + "\n"
	}
	return out
}

func (d ReviewDraft) Add(c ReviewComment) ReviewDraft {
	if d.Status == "" {
		d.Status = DraftOpen
	}
	d.Comments = append(append([]ReviewComment(nil), d.Comments...), c)
	return d
}

func (d ReviewDraft) Queue() ReviewDraft {
	if len(d.Comments) > 0 && d.Status != DraftSent {
		d.Status = DraftQueued
	}
	if d.Status == "" {
		d.Status = DraftOpen
	}
	return d
}

func (d ReviewDraft) Dispatch(s Session, now time.Time) (ReviewDraft, string, bool) {
	if d.Status != DraftQueued || !s.AcceptsSwitch() {
		return d, "", false
	}
	d.Status, d.SentAt = DraftSent, now
	return d, d.prompt(), true
}

func (d ReviewDraft) Unsend() ReviewDraft {
	d.Status, d.SentAt = DraftQueued, time.Time{}
	return d
}

func (d ReviewDraft) AwaitsTurn() bool { return d.Status == DraftSent && d.Turns == nil }

func (d ReviewDraft) LinkTurn(refs []string) ReviewDraft {
	d.Turns = append([]string{}, refs...)
	return d
}

type HunkAction string

const (
	HunkStage  HunkAction = "stage"
	HunkRevert HunkAction = "revert"
)

var ErrHunkUnsupported = errors.New("stage and revert work on plain text files only, not renames, binaries or quoted paths")

func HunkPatch(f FileDiff, h Hunk) (string, error) {
	if f.Binary || f.Status == FileRenamed || strings.HasPrefix(f.Path, `"`) || h.Header == "" {
		return "", ErrHunkUnsupported
	}
	var b strings.Builder
	b.WriteString("diff --git a/" + f.Path + " b/" + f.Path + "\n")
	from, to := "a/"+f.Path, "b/"+f.Path
	mode := f.Mode
	if mode == "" {
		mode = "100644"
	}
	switch f.Status {
	case FileAdded:
		b.WriteString("new file mode " + mode + "\n")
		from = "/dev/null"
	case FileDeleted:
		b.WriteString("deleted file mode " + mode + "\n")
		to = "/dev/null"
	}
	b.WriteString("--- " + from + "\n+++ " + to + "\n" + h.Header + "\n")
	for _, l := range h.Lines {
		b.WriteString(string(l.Kind) + l.Text + "\n")
		if l.NoEOL {
			b.WriteString("\\ No newline at end of file\n")
		}
	}
	return b.String(), nil
}
