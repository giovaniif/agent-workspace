package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

type ReviewScope string

const (
	ScopeLastTurn    ReviewScope = "last_turn"
	ScopeUncommitted ReviewScope = "uncommitted"
	ScopeBranch      ReviewScope = "branch"
)

var ReviewScopes = []ReviewScope{ScopeLastTurn, ScopeUncommitted, ScopeBranch}

var (
	ErrNoTurn          = errors.New("no prompt since the review began")
	ErrNoDefaultBranch = errors.New("the repo has no origin default branch")
	ErrUnknownScope    = errors.New("unknown review scope")
)

func (s ReviewScope) Shift(delta int) ReviewScope {
	i := 0
	for j, x := range ReviewScopes {
		if x == s {
			i = j
		}
	}
	n := len(ReviewScopes)
	return ReviewScopes[((i+delta)%n+n)%n]
}

type RangeFacts struct {
	LatestTurn    string
	DefaultBranch string
}

type DiffRange struct {
	From      string
	MergeBase bool
}

func RangeFor(scope ReviewScope, f RangeFacts) (DiffRange, error) {
	switch scope {
	case ScopeLastTurn:
		if f.LatestTurn == "" {
			return DiffRange{}, ErrNoTurn
		}
		return DiffRange{From: f.LatestTurn}, nil
	case ScopeUncommitted:
		return DiffRange{From: "HEAD"}, nil
	case ScopeBranch:
		if f.DefaultBranch == "" {
			return DiffRange{}, ErrNoDefaultBranch
		}
		return DiffRange{From: "origin/" + f.DefaultBranch, MergeBase: true}, nil
	}
	return DiffRange{}, ErrUnknownScope
}

const turnPrefix = "refs/crew/turns/"

func WorktreeKey(path string) string {
	sum := sha1.Sum([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:6])
}

func TurnRef(session, worktree string, n int) string {
	return turnPrefix + refSafe(session) + "/" + WorktreeKey(worktree) + "/" + strconv.Itoa(n)
}

func refSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		ok := r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			r = '-'
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), ".")
}

type turn struct {
	ref     string
	session string
	key     string
	n       int
	numeric bool
}

func parseTurn(ref string) (turn, bool) {
	rest, ok := strings.CutPrefix(ref, turnPrefix)
	if !ok {
		rest, ok = strings.CutPrefix(ref, LegacyTurnPrefix)
	}
	if !ok {
		return turn{}, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return turn{}, false
	}
	n, err := strconv.Atoi(parts[2])
	return turn{ref: ref, session: parts[0], key: parts[1], n: n, numeric: err == nil}, true
}

func LatestTurn(refs []string, session, worktree string) (string, int) {
	best, bestN := "", 0
	for _, t := range turnsOf(refs, session, worktree) {
		if t.numeric && t.n > bestN {
			best, bestN = t.ref, t.n
		}
	}
	return best, bestN
}

func OlderTurns(refs []string, session, worktree string, n int) []string {
	var out []string
	for _, t := range turnsOf(refs, session, worktree) {
		if t.numeric && t.n < n {
			out = append(out, t.ref)
		}
	}
	return out
}

func TurnsOfWorktree(refs []string, worktree string) []string {
	key := WorktreeKey(worktree)
	var out []string
	for _, r := range refs {
		if t, ok := parseTurn(r); ok && t.key == key {
			out = append(out, r)
		}
	}
	return out
}

func turnsOf(refs []string, session, worktree string) []turn {
	key, s := WorktreeKey(worktree), refSafe(session)
	var out []turn
	for _, r := range refs {
		if t, ok := parseTurn(r); ok && t.key == key && t.session == s {
			out = append(out, t)
		}
	}
	return out
}

type FileStatus string

const (
	FileAdded    FileStatus = "A"
	FileModified FileStatus = "M"
	FileDeleted  FileStatus = "D"
	FileRenamed  FileStatus = "R"
)

type LineKind byte

const (
	LineContext LineKind = ' '
	LineAdded   LineKind = '+'
	LineDeleted LineKind = '-'
)

type DiffLine struct {
	Kind  LineKind
	Old   int
	New   int
	Text  string
	NoEOL bool `json:",omitempty"`
}

type Hunk struct {
	Header string
	Lines  []DiffLine
}

type FileDiff struct {
	Path    string
	OldPath string
	Status  FileStatus
	Added   int
	Deleted int
	Binary  bool
	Blob    string
	Mode    string `json:",omitempty"`
	Hunks   []Hunk
}

func ParseDiff(out string) []FileDiff {
	var files []FileDiff
	var f *FileDiff
	var h *Hunk
	oldN, newN := 0, 0
	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")
		if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
			files = append(files, FileDiff{Status: FileModified, Path: headerPath(rest)})
			f, h = &files[len(files)-1], nil
			continue
		}
		if f == nil {
			continue
		}
		if h != nil {
			switch {
			case strings.HasPrefix(line, "+"):
				h.Lines = append(h.Lines, DiffLine{Kind: LineAdded, New: newN, Text: line[1:]})
				newN++
				f.Added++
				continue
			case strings.HasPrefix(line, "-"):
				h.Lines = append(h.Lines, DiffLine{Kind: LineDeleted, Old: oldN, Text: line[1:]})
				oldN++
				f.Deleted++
				continue
			case strings.HasPrefix(line, " "):
				h.Lines = append(h.Lines, DiffLine{Kind: LineContext, Old: oldN, New: newN, Text: line[1:]})
				oldN++
				newN++
				continue
			case strings.HasPrefix(line, `\`):
				if n := len(h.Lines); n > 0 {
					h.Lines[n-1].NoEOL = true
				}
				continue
			}
		}
		switch {
		case strings.HasPrefix(line, "@@ "):
			f.Hunks = append(f.Hunks, Hunk{Header: line})
			h = &f.Hunks[len(f.Hunks)-1]
			oldN, newN = hunkStarts(line)
		case strings.HasPrefix(line, "new file mode "):
			f.Status, f.Mode = FileAdded, strings.TrimPrefix(line, "new file mode ")
		case strings.HasPrefix(line, "deleted file mode "):
			f.Status, f.Mode = FileDeleted, strings.TrimPrefix(line, "deleted file mode ")
		case strings.HasPrefix(line, "rename from "):
			f.Status = FileRenamed
			f.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			f.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "index "):
			f.Blob = indexBlob(line)
		case strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		case strings.HasPrefix(line, "+++ "):
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				f.Path = strings.TrimPrefix(p, "b/")
			}
		case strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" && f.Status != FileRenamed {
				f.Path = strings.TrimPrefix(p, "a/")
			}
		}
	}
	return files
}

func headerPath(rest string) string {
	if i := strings.Index(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return rest
}

func indexBlob(line string) string {
	fields := strings.Fields(strings.TrimPrefix(line, "index "))
	if len(fields) == 0 {
		return ""
	}
	_, blob, _ := strings.Cut(fields[0], "..")
	return blob
}

func hunkStarts(header string) (int, int) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0
	}
	start := func(s string) int {
		num, count, hasCount := strings.Cut(s[1:], ",")
		n, _ := strconv.Atoi(num)
		if hasCount && count == "0" {
			return n + 1
		}
		return n
	}
	return start(fields[1]), start(fields[2])
}

type ViewedMark struct {
	Worktree string
	Path     string
	Blob     string
}

func (m ViewedMark) Key() string { return m.Worktree + "\x00" + m.Path }

func IsViewed(marks map[string]ViewedMark, worktree string, f FileDiff) bool {
	m, ok := marks[ViewedMark{Worktree: worktree, Path: f.Path}.Key()]
	return ok && m.Blob == f.Blob
}

type SplitRow struct {
	Left  *DiffLine
	Right *DiffLine
}

func SplitRows(h Hunk) []SplitRow {
	var rows []SplitRow
	lines := h.Lines
	for i := 0; i < len(lines); {
		if lines[i].Kind == LineContext {
			l := lines[i]
			rows = append(rows, SplitRow{Left: &l, Right: &l})
			i++
			continue
		}
		var dels, adds []DiffLine
		for i < len(lines) && lines[i].Kind == LineDeleted {
			dels = append(dels, lines[i])
			i++
		}
		for i < len(lines) && lines[i].Kind == LineAdded {
			adds = append(adds, lines[i])
			i++
		}
		for j := range max(len(dels), len(adds)) {
			var r SplitRow
			if j < len(dels) {
				r.Left = &dels[j]
			}
			if j < len(adds) {
				r.Right = &adds[j]
			}
			rows = append(rows, r)
		}
	}
	return rows
}

type WorktreeReview struct {
	Worktree Worktree
	From     string
	Files    []FileDiff
	Err      string
}
