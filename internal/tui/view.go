package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

var spinner = []string{"◐", "◓", "◑", "◒"}

type styles struct {
	text, sub, dim, bold, brand, header, need lipgloss.Style
	blue, peach, teal, green, mauve           lipgloss.Style
	bar, badge                                lipgloss.Style
	selectedBg, base                          lipgloss.Style
}

func newStyles(t Theme) styles {
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	return styles{
		text:       fg(t.Text),
		sub:        fg(t.Subtext),
		dim:        fg(t.Overlay),
		bold:       fg(t.Text).Bold(true),
		brand:      fg(t.Blue).Bold(true),
		header:     fg(t.Subtext).Bold(true),
		need:       fg(t.Peach).Bold(true),
		blue:       fg(t.Blue),
		peach:      fg(t.Peach),
		teal:       fg(t.Teal),
		green:      fg(t.Green),
		mauve:      fg(t.Mauve),
		bar:        fg(t.Blue),
		badge:      lipgloss.NewStyle().Foreground(lipgloss.Color(t.Base)).Background(lipgloss.Color(t.Blue)).Bold(true),
		selectedBg: lipgloss.NewStyle().Background(lipgloss.Color(t.Selected)),
		base:       lipgloss.NewStyle().Background(lipgloss.Color(t.Base)),
	}
}

func (m Model) harnessTag(h domain.Harness, pad string) piece {
	st := m.styles.blue
	switch h {
	case domain.HarnessCodex:
		st = m.styles.teal
	case domain.HarnessOmp:
		st = m.styles.mauve
	}
	return piece{st.Bold(true), pad + domain.Spec(h).Tag + pad}
}

type piece struct {
	st lipgloss.Style
	s  string
}

func (m Model) line(sel bool, left, right []piece) string {
	w := m.width
	render := func(ps []piece) (string, int) {
		var b strings.Builder
		n := 0
		for _, p := range ps {
			st := p.st
			if sel {
				st = st.Background(lipgloss.Color(m.opts.Theme.Selected))
			}
			b.WriteString(st.Render(p.s))
			n += ansi.StringWidth(p.s)
		}
		return b.String(), n
	}
	r, rw := render(right)
	room := w - rw
	if rw > 0 {
		room--
	}
	var plain []piece
	used := 0
	for _, p := range left {
		pw := ansi.StringWidth(p.s)
		if used+pw > room {
			if cut := room - used; cut > 0 {
				plain = append(plain, piece{p.st, ansi.Truncate(p.s, cut, "…")})
			}
			break
		}
		plain = append(plain, p)
		used += pw
	}
	l, lw := render(plain)
	gap := w - lw - rw
	if gap < 0 {
		gap = 0
	}
	pad := strings.Repeat(" ", gap)
	if sel {
		pad = m.styles.selectedBg.Render(pad)
	}
	return l + pad + r
}

func (m Model) View() tea.View {
	if m.ob != nil {
		return m.view(m.setupScreen())
	}
	if m.opts.SetupOnly {
		return tea.NewView("")
	}
	if m.opts.NewSessionOnly {
		if m.dialog == nil {
			return tea.NewView("")
		}
		return m.view(m.dialogScreen())
	}
	if m.rv.open {
		return m.view(m.reviewView())
	}
	if m.dk.open {
		return m.view(m.diskScreen())
	}
	lines, _ := m.mainScreen()
	return m.view(strings.Join(lines, "\n"))
}

func (m Model) mainScreen() (lines, owners []string) {
	s := m.styles
	lines = append(lines, m.topBar())
	lines = append(lines, m.limitLines()...)
	lines = append(lines, m.banner()...)
	lines = append(lines, "")
	projectAt := len(lines)
	projectLines, projectOwners := m.projectSection()
	lines = append(lines, projectLines...)

	need := 0
	for _, e := range m.entries {
		if e.session.NeedsYou() {
			need++
		}
	}
	var right []piece
	if need > 0 {
		right = []piece{{s.need, fmt.Sprintf("%d need you", need)}}
	}
	lines = append(lines, m.rule("SESSIONS", right))

	head := len(lines)
	body, selRow, bodyOwners := m.body()
	footer := m.footer()
	room := max(m.height-len(lines)-len(footer), 0)
	off := m.listOffset(selRow, len(body), room)
	for len(bodyOwners) < len(body) {
		bodyOwners = append(bodyOwners, "")
	}
	body, bodyOwners = body[off:], bodyOwners[off:]
	if len(body) > room {
		body, bodyOwners = body[:room], bodyOwners[:room]
	}
	owners = make([]string, len(lines), m.height)
	copy(owners[projectAt:], projectOwners)
	lines = append(lines, body...)
	owners = append(owners, bodyOwners...)
	for len(lines)+len(footer) < m.height {
		lines = append(lines, "")
	}
	lines = append(lines, footer...)
	for len(owners) < len(lines) {
		owners = append(owners, "")
	}
	if m.dialog != nil {
		lines, owners = m.overlayMenu(lines, owners, head+m.dialog.menuAt-off, m.dialog.menuCol)
	}
	return lines, owners
}

func (m Model) listOffset(selRow, rows, room int) int {
	off := 0
	switch {
	case m.scrolled:
		off = m.scroll
	case selRow >= room:
		off = selRow - room + 1 + min(2, max(room-1, 0))
	}
	return min(max(off, 0), max(0, rows-room))
}

func (m Model) listGeometry() (owners []string, off int) {
	projectLines, _ := m.projectSection()
	top := 1 + len(m.limitLines()) + len(m.banner()) + 2 + len(projectLines)
	body, selRow, owners := m.body()
	room := max(m.height-top-len(m.footer()), 0)
	return owners, m.listOffset(selRow, len(body), room)
}

func (m Model) view(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	if !m.opts.NoMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m Model) topBar() string {
	s := m.styles
	left := []piece{{s.brand, " agentws"}}
	for _, slot := range []struct{ name, val string }{{"claude", m.top.Claude}, {"codex", m.top.Codex}} {
		if slot.val != "" {
			left = append(left, piece{s.bold, "  " + slot.name + " "}, piece{s.text, slot.val})
		}
	}
	var right []piece
	if m.top.Disk != "" {
		right = append(right, piece{s.sub, "disk " + m.top.Disk + "  "})
	}
	right = append(right, piece{s.bold, m.opts.Now().Format("15:04")})
	return m.line(false, left, right)
}

func (m Model) body() ([]string, int, []string) {
	s := m.styles
	if m.dialog != nil {
		return m.dialogLines()
	}
	if m.proj != nil {
		return m.projectPanelLines(), 0, nil
	}
	if m.launching != nil {
		return m.launcherLines(), 0, nil
	}
	if m.help {
		return m.helpLines(), 0, nil
	}
	if m.picker != nil {
		owners := []string{"", ""}
		if m.picker.query != "" && !m.picker.typed && m.picker.kind == domain.SwitchModel {
			owners = append(owners, "")
		}
		for i := range m.picker.shown() {
			owners = append(owners, ownPick+strconv.Itoa(i))
		}
		return m.pickerLines(), 0, owners
	}
	if m.resuming != nil {
		owners := []string{"", ""}
		for i := range m.resuming.choices {
			owners = append(owners, ownResume+strconv.Itoa(i))
		}
		return m.resumeLines(), 2 + m.resuming.cursor, owners
	}
	if len(m.entries) == 0 {
		return append([]string{
			"",
			m.line(false, []piece{{s.sub, " No sessions yet."}}, nil),
			m.line(false, []piece{{s.dim, " Sessions you start show up here."}}, nil),
		}, m.queueLines()...), 0, nil
	}
	var out, owners []string
	selRow := 0
	for _, e := range m.entries {
		own := ownSession + e.session.ID
		if e.groupStart {
			out = append(out, "", m.line(false, m.taskHeader(e.task), []piece{{s.dim, strings.Join(e.groupRepos, " ")}}))
			owners = append(owners, "", own)
		}
		sel := e.session.ID == m.selected
		if sel {
			selRow = len(out)
		}
		card := m.sessionLines(e, sel)
		out = append(out, card...)
		for range card {
			owners = append(owners, own)
		}
	}
	return append(out, m.queueLines()...), selRow, owners
}

func (m Model) sessionLines(e entry, sel bool) []string {
	s := m.styles
	bar := piece{s.text, " "}
	if sel {
		bar = piece{s.bar, "▌"}
	}
	x := e.session
	tag := m.harnessTag(x.Harness, "")
	name := domain.NameFor(e.task, entryPRs(e))
	if name == "" {
		name = x.ID
	}
	right := m.muteMarker(x)
	if label := rowPortLabel(e.ports()); label != "" {
		right = append(right, piece{s.teal, label + " "})
	}
	out := []string{m.line(sel,
		[]piece{bar, m.glyph(x), {s.dim, fmt.Sprintf(" %d ", e.num)}, {s.bold, name}},
		append(right, tag, piece{s.text, " "}))}
	if sel {
		trees := "root only"
		if n := len(e.worktrees); n > 0 {
			trees = count(n, "worktree")
		}
		facts := slices.DeleteFunc([]string{x.Model, x.Effort, trees}, func(f string) bool { return f == "" })
		out = append(out, m.line(sel, []piece{bar, {s.dim, "    " + strings.Join(facts, " · ")}}, m.switchMarks(x)))
	}
	if m.collapsed[x.ID] {
		return out
	}
	return append(out, m.subagentLines(x.ID, sel)...)
}

func (m Model) muteMarker(x domain.Session) []piece {
	if !x.Muted {
		return nil
	}
	return []piece{{m.styles.dim, "⊘ "}}
}

func (m Model) glyph(x domain.Session) piece {
	s := m.styles
	switch {
	case x.State == domain.StateRunning:
		return piece{s.blue, spinner[m.frame%len(spinner)]}
	case x.NeedsYou():
		return piece{s.peach, "✳"}
	case x.State == domain.StateDone && x.Unread:
		return piece{s.peach, "●"}
	}
	return piece{s.dim, "○"}
}

func (m Model) helpLines() []string {
	s := m.styles
	keys := [][2]string{
		{"1-9", "jump to session"},
		{"space", "next waiting"},
		{"tab", "last session"},
		{"j / k", "move"},
		{"o", "show / hide subagents"},
		{"m", "mute session"},
		{"R", "rename and pin the name"},
		{"A", "unpin (name is automatic)"},
		{"K", "kill the session's dev servers"},
		{"r", "review the session's changes"},
		{"w", "worktrees and disk"},
		{"t / T", "shell below / popup"},
		{"s", "type in the shell (ctrl+\\ back)"},
		{"e", "nvim (o in a review opens the line)"},
		{"M / E", "switch model / effort"},
		{"enter", "focus agent pane"},
		{`ctrl+\`, "in an agent pane: back to the sidebar"},
		{"n", "new session"},
		{"L", "launch Linear issues"},
		{"P", "projects: open, add, remove"},
		{"c", "queued issues to codex"},
		{"X", "clear the queue"},
		{"S", "set up Claude, Codex and nvim"},
		{"x", "end session"},
		{"u", "resume ended"},
		{"?", "close help"},
		{"q", "leave (sessions keep running)"},
	}
	out := []string{"", m.line(false, []piece{{s.header, " KEYS"}}, nil)}
	for _, k := range keys {
		out = append(out, m.line(false, []piece{{s.bold, fmt.Sprintf(" %-7s", k[0])}, {s.sub, k[1]}}, nil))
	}
	return out
}

func (m Model) footer() []string {
	s := m.styles
	worktrees := 0
	var ports []domain.Port
	for _, e := range m.entries {
		worktrees += len(e.session.WorktreeIDs)
		ports = append(ports, e.ports()...)
	}
	counts := count(len(m.entries), "session") + " · " + count(worktrees, "worktree")
	if label := portLabel(ports); label != "" {
		counts += " · ports " + strings.ReplaceAll(label, ":", "")
	}
	left, withCounts := m.statusLeft()
	var right []piece
	if withCounts {
		left = []piece{{s.dim, " " + counts}}
		if ansi.StringWidth(counts)+16 <= m.width {
			right = []piece{{s.bold, "␣"}, {s.dim, " next waiting "}}
		}
	}
	if m.status != "" {
		right = []piece{{s.peach, m.status + " "}}
	}
	var hints []piece
	for _, h := range [][2]string{{"n", "new"}, {"r", "review"}, {"t", "shell"}, {"e", "nvim"}, {"?", "keys"}} {
		hints = append(hints, piece{s.bold, " " + h[0]}, piece{s.dim, " " + h[1] + " "})
	}
	return []string{m.line(false, hints, nil), m.line(false, left, right)}
}

func (m Model) statusLeft() (left []piece, withCounts bool) {
	s := m.styles
	if m.renaming != nil {
		return m.renameLeft(), false
	}
	if m.confirm != nil {
		return []piece{{s.badge, " KILL "}, {s.peach, " kill " + m.confirm.label + "? y/n"}}, false
	}
	if i := m.index(m.selected); i >= 0 {
		if label := portLabel(m.entries[i].ports()); label != "" {
			return []piece{{s.teal, " " + label}, {s.dim, " · K kill"}}, false
		}
	}
	return nil, true
}

func taskLabel(t domain.Task) string {
	title := t.PinnedName
	for _, c := range []string{t.IssueTitle, t.Text} {
		if title == "" {
			title = c
		}
	}
	switch {
	case t.Ref != "" && title != "":
		return t.Ref + " · " + title
	case t.Ref != "":
		return t.Ref
	case title != "":
		return title
	}
	return t.ID
}

func (m Model) taskHeader(t domain.Task) []piece {
	s := m.styles
	title := strings.TrimPrefix(taskLabel(t), t.Ref+" · ")
	if t.Ref == "" || title == t.Ref {
		return []piece{{s.sub, " " + title}}
	}
	return []piece{{s.dim, " " + t.Ref + " "}, {s.sub, title}}
}

func (m Model) rule(title string, right []piece) string {
	s := m.styles
	used := len(title) + 2
	for _, p := range right {
		used += ansi.StringWidth(p.s) + 1
	}
	return m.line(false, []piece{{s.dim, " " + title + " " + strings.Repeat("─", max(m.width-used-1, 0))}}, right)
}

func repoName(w domain.Worktree) string {
	if w.Repo == "" {
		return ""
	}
	return filepath.Base(w.Repo)
}

func worktreeLabel(w domain.Worktree) string {
	repo := repoName(w)
	part := w.SubtaskSlug
	if part == "" {
		part = w.Branch
	}
	if part == "" {
		return repo
	}
	return repo + ":" + part
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func cleanText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
