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

// why: owners names what each row shows, so a click can be mapped back to it.
func (m Model) mainScreen() (lines, owners []string) {
	s := m.styles
	lines = append(lines, m.topBar())
	lines = append(lines, m.limitLines()...)
	lines = append(lines, "")

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
	lines = append(lines, m.line(false, []piece{{s.header, " SESSIONS"}}, right))

	head := len(lines)
	body, selRow, bodyOwners := m.body()
	footer := append(m.cardLines(), m.footer()...)
	room := m.height - len(lines) - len(footer)
	if room < 0 {
		room = 0
	}
	off := 0
	if selRow >= room {
		off = selRow - room + 3
	}
	if off > len(body)-room {
		off = max(0, len(body)-room)
	}
	for len(bodyOwners) < len(body) {
		bodyOwners = append(bodyOwners, "")
	}
	body, bodyOwners = body[off:], bodyOwners[off:]
	if len(body) > room {
		body, bodyOwners = body[:room], bodyOwners[:room]
	}
	owners = make([]string, len(lines), m.height)
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
	left := []piece{{s.bar, "▌"}, {s.brand, "agentws"}}
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
		return m.resumeLines(), 2 + m.resuming.cursor, nil
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
			out = append(out, "", m.line(false, []piece{{s.sub, " " + taskLabel(e.task)}}, []piece{{s.sub, strings.Join(e.groupRepos, " ")}}))
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
	glyph := m.glyph(x)
	tag := m.harnessTag(x.Harness, "")
	name := domain.NameFor(e.task, entryPRs(e))
	if name == "" {
		name = x.ID
	}
	out := []string{m.line(sel,
		[]piece{bar, glyph, {s.text, fmt.Sprintf(" %d ", e.num)}, {s.bold, name}},
		append(m.muteMarker(x), tag, piece{s.text, " "}))}

	detail := strings.TrimSpace(x.Model + " " + x.Effort)
	trees := "root only"
	if n := len(x.WorktreeIDs); n > 0 {
		trees = count(n, "worktree")
	}
	var open []piece
	if label := portLabel(e.ports()); label != "" {
		open = []piece{{s.teal, label}, {s.text, " "}}
	}
	facts := []string{detail}
	if x.Usage.HasContext {
		facts = append(facts, fmt.Sprintf("ctx %d%%", x.Usage.ContextLeftPercent))
	}
	facts = append(facts, trees)
	row := strings.Join(slices.DeleteFunc(facts, func(f string) bool { return f == "" }), "  ")
	out = append(out, m.line(sel, []piece{bar, {s.sub, "    " + row}}, append(open, m.switchMarks(x)...)))
	if m.collapsed[x.ID] {
		return out
	}
	for _, w := range e.worktrees {
		pr := piece{s.dim, "–"}
		if w.PR != nil {
			pr = piece{s.sub, fmt.Sprintf("#%d", w.PR.Number)}
		}
		right := []piece{pr, {s.text, "   "}}
		if label := portLabel(w.Ports); label != "" {
			right = append([]piece{{s.teal, label + " "}}, right...)
		}
		out = append(out, m.line(sel,
			[]piece{bar, {s.dim, "     └ "}, {s.text, worktreeLabel(w)}},
			right))
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
		{"o", "expand / collapse worktrees"},
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
	right := []piece{{s.sub, counts + " "}}
	left, withCounts := m.statusLeft()
	switch {
	case m.status != "":
		right = []piece{{s.peach, m.status + " "}}
	case !withCounts:
		right = nil
	}
	return []string{
		m.keyRow("n", "new session", "r", "review"),
		m.keyRow("t", "shell", "e", "nvim"),
		m.keyRow("w", "worktrees", "␣", "next waiting"),
		m.line(false, left, right),
	}
}

func (m Model) keyRow(k1, what1, k2, what2 string) string {
	s := m.styles
	return m.line(false, []piece{{s.bold, " " + k1}, {s.sub, fmt.Sprintf(" %-14s", what1)}, {s.bold, k2}, {s.sub, " " + what2}}, nil)
}

// why: ports and the kill prompt need the room the counts would take, so they
// leave the counts out.
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
			return []piece{{s.badge, " SESSION "}, {s.teal, " " + label}, {s.sub, " · K kill"}}, false
		}
	}
	return []piece{{s.badge, " SESSION "}, {s.sub, " j/k move · ? keys"}}, true
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

func repoName(w domain.Worktree) string {
	if w.Repo == "" {
		return ""
	}
	// why: Repo is the main checkout's path; its last element is the name people use.
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

const minCardHeight = 24

func (m Model) cardLines() []string {
	i := m.index(m.selected)
	if i < 0 || m.help || m.height < minCardHeight {
		return nil
	}
	s := m.styles
	e := m.entries[i]
	card := domain.BuildSessionCard(e.task, e.session, e.worktrees, m.events[e.session.ID])
	out := []string{"", m.line(false, []piece{{s.header, " CARD"}}, nil)}

	var title []string
	for _, part := range []string{card.Ref, card.Title} {
		if part != "" {
			title = append(title, part)
		}
	}
	label := strings.Join(title, " · ")
	if label == "" {
		label = e.session.ID
	}
	out = append(out, m.line(false, []piece{{s.bold, " " + label}}, nil))
	out = append(out, m.nameLines(cleanText(card.Name))...)

	if len(card.PRs) > 0 {
		chips := []piece{{s.dim, " PRs "}}
		for _, pr := range card.PRs {
			chips = append(chips, piece{s.blue, fmt.Sprintf("#%d ", pr.Number)})
		}
		out = append(out, m.line(false, chips, nil))
		out = append(out, m.prBoardLines(card.PRs)...)
	}
	for j, action := range card.Actions {
		lead := "       "
		if j == 0 {
			lead = " last  "
		}
		out = append(out, m.line(false, []piece{{s.dim, lead}, {s.sub, cleanText(action)}}, nil))
	}
	if card.Waiting != "" {
		reason := "waiting on you"
		if e.session.State == domain.StatePermission {
			reason = "asks permission"
		}
		out = append(out, m.line(false, []piece{{s.need, " ✳ " + reason}}, nil))
		for _, line := range strings.Split(card.Waiting, "\n") {
			out = append(out, m.line(false, []piece{{s.text, "   " + cleanText(line)}}, nil))
		}
	}
	return out
}

func (m Model) prBoardLines(prs []domain.PullRequest) []string {
	s := m.styles
	var out []string
	for _, pr := range prs {
		if pr.State == "" {
			continue
		}
		head := fmt.Sprintf(" #%d ", pr.Number)
		switch {
		case pr.State != domain.PROpen:
			out = append(out, m.line(false, []piece{{s.blue, head}, {s.dim, strings.ToLower(string(pr.State))}}, nil))
			continue
		case pr.ReadyToMerge():
			out = append(out, m.line(false, []piece{{s.blue, head}, {s.green, "ready to merge"}}, nil))
		default:
			out = append(out, m.line(false, []piece{{s.blue, head}, {s.need, "blocked"}}, nil))
			for _, b := range pr.Blockers() {
				out = append(out, m.line(false, []piece{{s.sub, "   " + b}}, nil))
			}
		}
		for _, f := range pr.Failing {
			out = append(out, m.line(false, []piece{{s.peach, "   ✗ " + link(f.URL, cleanText(f.Name))}}, nil))
		}
		if pr.BotComments > 0 {
			out = append(out, m.line(false, []piece{{s.dim, "   " + count(pr.BotComments, "bot comment") + " since push"}}, nil))
		}
	}
	return out
}

func (m Model) nameLines(name string) []string {
	if name == "" {
		return nil
	}
	const lead = " name  "
	var out []string
	for i, part := range strings.Split(ansi.Wrap(name, max(m.width-len(lead), 1), ""), "\n") {
		prefix := strings.Repeat(" ", len(lead))
		if i == 0 {
			prefix = lead
		}
		out = append(out, m.line(false, []piece{{m.styles.dim, prefix}, {m.styles.text, part}}, nil))
	}
	return out
}

// why: the URL comes from GitHub, so anything with a control character or a
// scheme other than http(s) is left as text rather than risk breaking out of
// the OSC 8 sequence.
func link(url, text string) string {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return text
	}
	for _, r := range url {
		if r < ' ' || r == 0x7f {
			return text
		}
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// why: drops escape sequences and control characters an agent put in its
// text, so they cannot repaint the terminal.
func cleanText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
