package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type Reviewer interface {
	Review(ctx context.Context, p rpc.ReviewParams) (rpc.Review, error)
	MarkViewed(ctx context.Context, mark domain.ViewedMark, viewed bool) error
	AddReviewComment(ctx context.Context, p rpc.CommentParams) (domain.ReviewDraft, error)
	SendReview(ctx context.Context, session string) (domain.ReviewDraft, error)
	ApplyHunk(ctx context.Context, p rpc.HunkParams) error
	ReviewLayout(ctx context.Context, open bool) error
}

const reviewCallTimeout = 15 * time.Second

type styledLine struct {
	line domain.DiffLine
	toks []tok
}

type diffRow struct {
	hunk   int
	header string
	one    *styledLine
	left   *styledLine
	right  *styledLine
}

type reviewFile struct {
	wt      domain.Worktree
	file    domain.FileDiff
	unified []diffRow
	split   []diffRow
}

type treeRow struct {
	wt   domain.Worktree
	err  string
	file int
}

type prepared struct {
	files []reviewFile
	tree  []treeRow
}

type reviewMsg struct {
	seq    int
	review rpc.Review
	prep   prepared
	err    error
}

type reviewState struct {
	open     bool
	session  string
	scope    domain.ReviewScope
	worktree string
	split    bool
	seq      int
	loading  bool
	err      string

	review rpc.Review
	prep   prepared
	marks  map[string]domain.ViewedMark
	cur    int
	scroll int

	line     int
	mark     int
	marking  bool
	dragging bool
	typing   bool
	text     string
	confirm  bool
}

func prepare(syntax syntaxColors, r rpc.Review) prepared {
	var p prepared
	for _, w := range r.Worktrees {
		p.tree = append(p.tree, treeRow{wt: w.Worktree, err: w.Err, file: -1})
		for _, f := range w.Files {
			p.tree = append(p.tree, treeRow{wt: w.Worktree, file: len(p.files)})
			p.files = append(p.files, layoutFile(syntax, w.Worktree, f))
		}
	}
	return p
}

func layoutFile(syntax syntaxColors, wt domain.Worktree, f domain.FileDiff) reviewFile {
	rf := reviewFile{wt: wt, file: f}
	var all []domain.DiffLine
	for _, h := range f.Hunks {
		all = append(all, h.Lines...)
	}
	toks := highlight(syntax, f.Path, all)
	styled := map[domain.DiffLine]*styledLine{}
	i := 0
	for hi, h := range f.Hunks {
		rf.unified = append(rf.unified, diffRow{hunk: hi, header: h.Header})
		rf.split = append(rf.split, diffRow{hunk: hi, header: h.Header})
		for _, l := range h.Lines {
			sl := &styledLine{line: l, toks: toks[i]}
			styled[l] = sl
			i++
			rf.unified = append(rf.unified, diffRow{hunk: hi, one: sl})
		}
		for _, r := range domain.SplitRows(h) {
			row := diffRow{hunk: hi}
			if r.Left != nil {
				row.left = styled[*r.Left]
			}
			if r.Right != nil {
				row.right = styled[*r.Right]
			}
			rf.split = append(rf.split, row)
		}
	}
	return rf
}

func (m Model) openReview() (tea.Model, tea.Cmd) {
	r := m.opts.Review
	if r == nil || m.selected == "" {
		return m, nil
	}
	if m.rv.scope == "" {
		m.rv.scope = domain.ScopeUncommitted
	}
	if m.rv.session != m.selected {
		m.rv.worktree, m.rv.cur, m.rv.scroll = "", 0, 0
	}
	m.rv.open, m.rv.session = true, m.selected
	fetch := m.fetchReview()
	return m, tea.Batch(layout(r, true), fetch)
}

func (m Model) closeReview() (tea.Model, tea.Cmd) {
	m.rv.open = false
	return m, layout(m.opts.Review, false)
}

func layout(r layouter, open bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := r.ReviewLayout(ctx, open); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *Model) fetchReview() tea.Cmd {
	m.rv.seq++
	m.rv.loading = true
	r, seq, syntax := m.opts.Review, m.rv.seq, newSyntaxColors(m.opts.Theme)
	p := rpc.ReviewParams{Session: m.rv.session, Scope: m.rv.scope, Worktree: m.rv.worktree}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewCallTimeout)
		defer cancel()
		rev, err := r.Review(ctx, p)
		if err != nil {
			return reviewMsg{seq: seq, err: err}
		}
		return reviewMsg{seq: seq, review: rev, prep: prepare(syntax, rev)}
	}
}

func (m *Model) gotReview(msg reviewMsg) {
	if msg.seq != m.rv.seq {
		return
	}
	m.rv.loading = false
	if msg.err != nil {
		m.rv.err = msg.err.Error()
		m.rv.review, m.rv.prep = rpc.Review{Scope: m.rv.scope}, prepared{}
		m.rv.cur, m.rv.scroll = 0, 0
		m.resetLine()
		return
	}
	prevPath := ""
	if f, ok := m.rv.current(); ok {
		prevPath = f.wt.ID + "\x00" + f.file.Path
	}
	m.rv.err, m.rv.review, m.rv.prep = "", msg.review, msg.prep
	m.rv.marks = map[string]domain.ViewedMark{}
	for _, mk := range msg.review.Viewed {
		m.rv.marks[mk.Key()] = mk
	}
	if d := msg.review.Draft; d.ID != "" {
		d.Session = m.rv.session
		m.putDraft(d)
	}
	cur := 0
	for i, f := range m.rv.prep.files {
		if f.wt.ID+"\x00"+f.file.Path == prevPath {
			cur = i
		}
	}
	if cur != m.rv.cur || prevPath == "" {
		m.rv.cur, m.rv.scroll = cur, 0
		m.resetLine()
	}
	m.clampLine()
}

func (r reviewState) current() (reviewFile, bool) {
	if r.cur < 0 || r.cur >= len(r.prep.files) {
		return reviewFile{}, false
	}
	return r.prep.files[r.cur], true
}

func (r reviewState) viewed(f reviewFile) bool {
	return domain.IsViewed(r.marks, f.wt.ID, f.file)
}

func (m Model) reviewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch {
	case m.rv.typing:
		return m.commentKey(msg)
	case m.rv.confirm:
		m.rv.confirm, m.status = false, ""
		if k == "y" {
			return m, m.applyHunk(domain.HunkRevert)
		}
		return m, nil
	}
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.rv.marking {
			m.rv.marking = false
			return m, nil
		}
		return m.closeReview()
	case "r":
		return m.closeReview()
	case "V":
		m.rv.marking, m.rv.mark = !m.rv.marking, m.rv.line
	case "c":
		m.rv.typing, m.rv.text = true, ""
	case "S":
		return m, m.sendDraft()
	case "s":
		return m, m.applyHunk(domain.HunkStage)
	case "x":
		if _, ok := m.cursorHunk(); ok {
			m.rv.confirm, m.status = true, "revert this hunk? y/n"
		}
	case "]", "[", "w":
		switch k {
		case "]":
			m.rv.scope = m.rv.scope.Shift(1)
		case "[":
			m.rv.scope = m.rv.scope.Shift(-1)
		default:
			m.rv.worktree = m.nextWorktree()
		}
		fetch := m.fetchReview()
		return m, fetch
	case "o":
		return m.openInNvim()
	case "u":
		m.rv.split = !m.rv.split
		m.rv.scroll = 0
		m.resetLine()
	case "v":
		mark := m.toggleViewed()
		return m, mark
	case "n":
		m.moveFile(1)
	case "p":
		m.moveFile(-1)
	case "j", "down":
		m.moveLine(1)
	case "k", "up":
		m.moveLine(-1)
	case "ctrl+d":
		m.moveLine(m.diffHeight() / 2)
	case "ctrl+u":
		m.moveLine(-m.diffHeight() / 2)
	}
	return m, nil
}

func (m Model) nextWorktree() string {
	ids := []string{""}
	for _, e := range m.entries {
		if e.session.ID == m.rv.session {
			for _, w := range e.worktrees {
				ids = append(ids, w.ID)
			}
		}
	}
	for i, id := range ids {
		if id == m.rv.worktree {
			return ids[(i+1)%len(ids)]
		}
	}
	return ""
}

func (m *Model) moveFile(delta int) {
	n := len(m.rv.prep.files)
	if n == 0 {
		return
	}
	next := min(max(m.rv.cur+delta, 0), n-1)
	if next != m.rv.cur {
		m.rv.cur, m.rv.scroll = next, 0
		m.resetLine()
	}
}

func (m *Model) toggleViewed() tea.Cmd {
	f, ok := m.rv.current()
	if !ok {
		return nil
	}
	mark := domain.ViewedMark{Worktree: f.wt.ID, Path: f.file.Path, Blob: f.file.Blob}
	viewed := !m.rv.viewed(f)
	marks := make(map[string]domain.ViewedMark, len(m.rv.marks)+1)
	for k, v := range m.rv.marks {
		marks[k] = v
	}
	if viewed {
		marks[mark.Key()] = mark
	} else {
		delete(marks, mark.Key())
	}
	m.rv.marks = marks
	r := m.opts.Review
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := r.MarkViewed(ctx, mark, viewed); err != nil {
			return errMsg{err}
		}
		return nil
	}
}
