package tui

import (
	"fmt"
	"path"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/crew/internal/domain"
)

type seg struct {
	text string
	fg   string
	bold bool
}

type painter struct {
	theme  Theme
	styles map[[2]string]lipgloss.Style
	bolds  map[[2]string]lipgloss.Style
}

func newPainter(t Theme) *painter {
	return &painter{theme: t, styles: map[[2]string]lipgloss.Style{}, bolds: map[[2]string]lipgloss.Style{}}
}

func (p *painter) style(fg, bg string, bold bool) lipgloss.Style {
	if fg == "" {
		fg = p.theme.Text
	}
	cache := p.styles
	if bold {
		cache = p.bolds
	}
	k := [2]string{fg, bg}
	if st, ok := cache[k]; ok {
		return st
	}
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Bold(bold)
	if bg != "" {
		st = st.Background(lipgloss.Color(bg))
	}
	cache[k] = st
	return st
}

func (p *painter) cell(width int, bg string, segs ...seg) string {
	var b strings.Builder
	used := 0
	for _, s := range segs {
		if used >= width {
			break
		}
		text := s.text
		if w := ansi.StringWidth(text); used+w > width {
			text = ansi.Truncate(text, width-used, "")
		}
		used += ansi.StringWidth(text)
		b.WriteString(p.style(s.fg, bg, s.bold).Render(text))
	}
	if used < width {
		b.WriteString(p.style("", bg, false).Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

func (p *painter) cellRight(width int, bg string, left, right []seg) string {
	rw := 0
	for _, s := range right {
		rw += ansi.StringWidth(s.text)
	}
	if rw >= width {
		return p.cell(width, bg, right...)
	}
	return p.cell(width-rw, bg, left...) + p.cell(rw, bg, right...)
}

const (
	railWidth    = 5
	treeMax      = 34
	reviewChrome = 5
)

func (m Model) reviewWidths() (area, tree, diff int) {
	area = max(m.width-railWidth-1, 20)
	if agent := m.agentColumnWidth(); agent > 0 {
		area -= agent + 1
	}
	tree = min(treeMax, area/3)
	return area, tree, area - tree - 1
}

func (m Model) diffHeight() int { return max(m.height-reviewChrome-1, 1) }

func (m Model) reviewView() string {
	t := m.opts.Theme
	p := m.paint
	area, treeW, diffW := m.reviewWidths()
	bodyH := max(m.height-reviewChrome, 1)

	var lines []string
	lines = append(lines, m.topBar())
	right := []string{m.scopeBar(area), m.worktreeBar(area), p.cell(area, "", seg{text: strings.Repeat("─", area), fg: t.Surface})}
	tree, _ := m.treeLines(treeW, bodyH)
	diff := m.diffLines(diffW, bodyH)
	sep := p.style(t.Surface, "", false).Render("│")
	for i := range bodyH {
		right = append(right, tree[i]+sep+diff[i])
	}
	rail := m.railLines(len(right) + 1)
	footerWidth := area
	if w := m.agentColumnWidth(); w > 0 {
		agent := m.agentColumn(w, len(right))
		for i := range right {
			right[i] = agent[i] + sep + right[i]
		}
		footerWidth += w + 1
	}
	right = append(right, m.reviewFooter(footerWidth))
	for i, r := range right {
		lines = append(lines, rail[i]+sep+r)
	}
	return strings.Join(lines, "\n")
}

func (m Model) railLines(n int) []string {
	t := m.opts.Theme
	p := m.paint
	out := make([]string, 0, n)
	for _, e := range m.entries {
		bar, bg := " ", ""
		if e.session.ID == m.rv.session {
			bar, bg = "▌", t.Selected
		}
		g := m.glyph(e.session)
		fg := t.Overlay
		switch g.s {
		case "✳", "●":
			fg = t.Peach
		case "○":
		default:
			fg = t.Blue
		}
		out = append(out,
			p.cell(railWidth, bg, seg{text: bar, fg: t.Blue}, seg{text: " " + g.s, fg: fg}),
			p.cell(railWidth, bg, seg{text: bar, fg: t.Blue}, seg{text: fmt.Sprintf(" %d", e.num), fg: t.Subtext}),
			p.cell(railWidth, ""))
	}
	for len(out) < n {
		out = append(out, p.cell(railWidth, ""))
	}
	return out[:n]
}

func scopeLabel(s domain.ReviewScope) string {
	switch s {
	case domain.ScopeLastTurn:
		return "last turn"
	case domain.ScopeBranch:
		return "branch vs base"
	}
	return "uncommitted"
}

func (m Model) chip(label string, on bool) seg {
	if on {
		return seg{text: " " + label + " ", fg: m.opts.Theme.Base, bold: true}
	}
	return seg{text: " " + label + " ", fg: m.opts.Theme.Text}
}

func (m Model) chips(lead seg, labels []string, active int, onBg string, rightSegs []seg, width int) string {
	t := m.opts.Theme
	p := m.paint
	var b strings.Builder
	used := ansi.StringWidth(lead.text)
	b.WriteString(p.style(lead.fg, "", lead.bold).Render(lead.text))
	rw := 0
	for _, s := range rightSegs {
		rw += ansi.StringWidth(s.text)
	}
	for i, l := range labels {
		c := m.chip(l, i == active)
		w := ansi.StringWidth(c.text) + 1
		if used+w > width-rw {
			break
		}
		bg := t.Mantle
		if i == active {
			bg = onBg
		}
		b.WriteString(p.style(c.fg, bg, c.bold).Render(c.text))
		b.WriteString(" ")
		used += w
	}
	return b.String() + p.cellRight(width-used, "", nil, rightSegs)
}

func (m Model) scopeBar(width int) string {
	t := m.opts.Theme
	files, add, del, viewed := 0, 0, 0, 0
	for _, f := range m.rv.prep.files {
		files++
		add += f.file.Added
		del += f.file.Deleted
		if m.rv.viewed(f) {
			viewed++
		}
	}
	labels, active := m.scopeChips()
	right := []seg{
		{text: count(files, "file") + "  ", fg: t.Subtext},
		{text: fmt.Sprintf("+%d", add), fg: t.Green},
		{text: "  "},
		{text: fmt.Sprintf("-%d", del), fg: t.Red},
		{text: fmt.Sprintf("  %d/%d viewed ", viewed, files), fg: t.Subtext},
	}
	if n := m.draftCount(m.rv.session); n > 0 {
		label := count(n, "draft comment")
		if m.drafts[m.rv.session].Status == domain.DraftQueued {
			label += " queued"
		}
		right = append([]seg{{text: label + "  ", fg: t.Mauve, bold: true}}, right...)
	}
	if m.rv.loading {
		right = append([]seg{{text: "loading…  ", fg: t.Overlay}}, right...)
	}
	return m.chips(seg{text: scopeLead, fg: t.Subtext, bold: true}, labels, active, t.Blue, right, width)
}

const (
	scopeLead    = " REVIEW  "
	worktreeLead = " worktree  "
)

func (m Model) scopeChips() (labels []string, active int) {
	labels = make([]string, len(domain.ReviewScopes))
	for i, s := range domain.ReviewScopes {
		labels[i] = scopeLabel(s)
		if s == domain.ScopeBranch {
			if b := m.sharedDefaultBranch(); b != "" {
				labels[i] = "branch vs " + b
			}
		}
		if s == m.rv.scope {
			active = i
		}
	}
	return labels, active
}

func (m Model) worktreeChips() (labels, ids []string, active int) {
	labels, ids = []string{"all"}, []string{""}
	for _, e := range m.entries {
		if e.session.ID != m.rv.session {
			continue
		}
		for _, w := range e.worktrees {
			if w.ID == m.rv.worktree {
				active = len(labels)
			}
			labels = append(labels, worktreeChip(w))
			ids = append(ids, w.ID)
		}
	}
	return labels, ids, active
}

func (m Model) sharedDefaultBranch() string {
	defaults := map[string]string{}
	for _, ws := range m.workspaces {
		for _, r := range ws.Repos {
			defaults[r.Path] = r.DefaultBranch
		}
	}
	shared := ""
	for _, e := range m.entries {
		if e.session.ID != m.rv.session {
			continue
		}
		for _, w := range e.worktrees {
			b := defaults[w.Repo]
			if b == "" || (shared != "" && b != shared) {
				return ""
			}
			shared = b
		}
	}
	return shared
}

func (m Model) worktreeBar(width int) string {
	t := m.opts.Theme
	labels, _, active := m.worktreeChips()
	return m.chips(seg{text: worktreeLead, fg: t.Subtext}, labels, active, t.Text, nil, width)
}

func worktreeChip(w domain.Worktree) string {
	if w.PR != nil {
		return fmt.Sprintf("%s #%d", worktreeLabel(w), w.PR.Number)
	}
	return worktreeLabel(w)
}

func statusColor(t Theme, s domain.FileStatus) string {
	switch s {
	case domain.FileAdded:
		return t.Green
	case domain.FileDeleted:
		return t.Red
	case domain.FileRenamed:
		return t.Blue
	}
	return t.Peach
}

func (m Model) treeLines(width, height int) ([]string, []int) {
	t := m.opts.Theme
	p := m.paint
	var rows []string
	var files []int
	curRow := 0
	for _, r := range m.rv.prep.tree {
		if r.file < 0 {
			if len(rows) > 0 {
				rows = append(rows, p.cell(width, ""))
			}
			var pr []seg
			if r.wt.PR != nil {
				pr = []seg{{text: fmt.Sprintf("#%d ", r.wt.PR.Number), fg: t.Subtext}}
			}
			rows = append(rows, p.cellRight(width, "", []seg{{text: " " + worktreeLabel(r.wt), bold: true}}, pr))
			if r.err != "" {
				rows = append(rows, p.cell(width, "", seg{text: "   ! " + r.err, fg: t.Overlay}))
			}
			for len(files) < len(rows) {
				files = append(files, -1)
			}
			continue
		}
		f := m.rv.prep.files[r.file]
		bg := ""
		if r.file == m.rv.cur {
			bg, curRow = t.Selected, len(rows)
		}
		mark := seg{text: "   "}
		if m.rv.viewed(f) {
			mark = seg{text: " ✓ ", fg: t.Green}
		}
		stat := fmt.Sprintf("+%d -%d ", f.file.Added, f.file.Deleted)
		if f.file.Deleted == 0 {
			stat = fmt.Sprintf("+%d ", f.file.Added)
		}
		rows = append(rows, p.cellRight(width, bg,
			[]seg{mark, {text: string(f.file.Status) + " ", fg: statusColor(t, f.file.Status), bold: true}, {text: path.Base(f.file.Path)}},
			[]seg{{text: stat, fg: t.Subtext}}))
		for len(files) < len(rows)-1 {
			files = append(files, -1)
		}
		files = append(files, r.file)
	}
	if len(m.rv.prep.files) == 0 && !m.rv.loading {
		msg := "  no changes"
		if m.rv.err != "" {
			msg = "  " + m.rv.err
		}
		rows = append(rows, p.cell(width, "", seg{text: msg, fg: t.Overlay}))
	}
	off := 0
	if curRow >= height {
		off = curRow - height + 1
	}
	for len(files) < len(rows) {
		files = append(files, -1)
	}
	rows, files = rows[min(off, len(rows)):], files[min(off, len(files)):]
	for len(rows) < height {
		rows = append(rows, p.cell(width, ""))
		files = append(files, -1)
	}
	return rows[:height], files[:height]
}

const numWidth = 5

func (m Model) diffLines(width, height int) []string {
	t := m.opts.Theme
	p := m.paint
	out := make([]string, 0, height)
	f, ok := m.rv.current()
	if !ok {
		for len(out) < height {
			out = append(out, p.cell(width, ""))
		}
		return out
	}
	unified, split := m.chip("unified", !m.rv.split), m.chip("split", m.rv.split)
	ubg, sbg := t.Surface, t.Base
	if m.rv.split {
		ubg, sbg = t.Base, t.Surface
	}
	unified.fg, split.fg = t.Text, t.Text
	head := []seg{{text: " "}, {text: " " + worktreeLabel(f.wt) + " ", fg: t.Subtext}, {text: "  " + f.file.Path + "  ", bold: true},
		{text: fmt.Sprintf("+%d", f.file.Added), fg: t.Green}, {text: "  "}, {text: fmt.Sprintf("-%d", f.file.Deleted), fg: t.Red}}
	toggle := p.cell(ansi.StringWidth(unified.text), ubg, unified) + " " + p.cell(ansi.StringWidth(split.text), sbg, split) + " "
	out = append(out, p.cell(width-ansi.StringWidth(ansi.Strip(toggle)), "", head...)+toggle)

	rows := f.unified
	if m.rv.split {
		rows = f.split
	}
	switch {
	case f.file.Binary:
		out = append(out, p.cell(width, "", seg{text: "  binary file", fg: t.Overlay}))
	case len(rows) == 0:
		out = append(out, p.cell(width, "", seg{text: "  no text changes", fg: t.Overlay}))
	}
	from, to := m.rv.selected()
	for i := m.rv.scroll; i < len(rows) && len(out) < height; i++ {
		out = append(out, m.diffRow(rows[i], width, i >= from && i <= to))
	}
	for len(out) < height {
		out = append(out, p.cell(width, ""))
	}
	return out
}

func (m Model) diffRow(r diffRow, width int, at bool) string {
	t := m.opts.Theme
	p := m.paint
	bar := seg{text: " "}
	if at {
		bar = seg{text: "▌", fg: t.Blue}
	}
	if r.header != "" {
		return p.cell(1, t.Mantle, bar) + p.cell(width-1, t.Mantle, seg{text: cleanText(r.header), fg: t.Subtext})
	}
	if r.one != nil {
		return p.cell(1, "", bar) + m.codeCell(r.one, width-1, sideBoth)
	}
	half := (width - 2) / 2
	return p.cell(1, "", bar) + m.codeCell(r.left, half, sideOld) + p.style(t.Surface, "", false).Render("│") + m.codeCell(r.right, width-2-half, sideNew)
}

type side int

const (
	sideBoth side = iota
	sideOld
	sideNew
)

func (m Model) codeCell(sl *styledLine, width int, sd side) string {
	t := m.opts.Theme
	p := m.paint
	if sl == nil {
		return p.cell(width, t.Mantle)
	}
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", numWidth)
		}
		return fmt.Sprintf("%*d", numWidth, n)
	}
	bg, sign, signFg := "", " ", t.Text
	switch sl.line.Kind {
	case domain.LineAdded:
		bg, sign, signFg = t.AddedBg, "+", t.Green
	case domain.LineDeleted:
		bg, sign, signFg = t.DeletedBg, "-", t.Red
	}
	var nums string
	switch sd {
	case sideBoth:
		nums = num(sl.line.Old) + " " + num(sl.line.New)
	case sideOld:
		nums = num(sl.line.Old)
	default:
		nums = num(sl.line.New)
	}
	segs := make([]seg, 0, len(sl.toks)+2)
	segs = append(segs, seg{text: nums, fg: t.Overlay}, seg{text: " " + sign + "  ", fg: signFg})
	for _, tk := range sl.toks {
		segs = append(segs, seg{text: tk.text, fg: tk.color})
	}
	return p.cell(width, bg, segs...)
}

func (m Model) reviewFooter(width int) string {
	t := m.opts.Theme
	if m.rv.typing {
		return m.paint.cell(width, "", seg{text: " comment: ", fg: t.Mauve, bold: true}, seg{text: m.rv.text + "▏"},
			seg{text: "   enter add · ctrl+j newline · esc cancel", fg: t.Subtext})
	}
	var segs []seg
	keys := [][2]string{{"c", "comment"}, {"V", "range"}, {"S", "send"}, {"s", "stage"}, {"x", "revert"}, {"v", "viewed"}, {"u", "split"}, {"[ ]", "scope"}, {"w", "worktree"}, {"n/p", "file"}, {"j/k", "line"}}
	if m.opts.Calls != nil {
		keys = append(keys, [2]string{"o", "nvim"})
	}
	keys = append(keys, [2]string{"r", "close"})
	for _, k := range keys {
		segs = append(segs, seg{text: " " + k[0], bold: true}, seg{text: " " + k[1] + "  ", fg: t.Subtext})
	}
	var right []seg
	if m.status != "" {
		right = []seg{{text: m.status + " ", fg: t.Peach}}
	}
	return m.paint.cellRight(width, "", segs, right)
}
