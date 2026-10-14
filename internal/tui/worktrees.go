package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type Disker interface {
	DiskView(ctx context.Context) (rpc.DiskView, error)
	CleanupWorktree(ctx context.Context, path string, backup bool) (rpc.CleanupItem, error)
	WorktreeShell(ctx context.Context, id string) error
	ReviewLayout(ctx context.Context, open bool) error
}

const (
	diskCallTimeout     = 15 * time.Second
	cleanupCallTimeout  = 5 * time.Minute
	pendingRefetchTicks = 10
	idleRefetchTicks    = 50
	recentShown         = 5
)

type diskKind int

const (
	diskRemove diskKind = iota
	diskBackupRemove
	diskKillPorts
)

type diskConfirm struct {
	kind   diskKind
	id     string
	label  string
	pgids  []int
	prompt string
}

type diskState struct {
	open    bool
	loading bool
	loaded  bool
	err     string
	view    rpc.DiskView
	sel     string
	seq     int
	since   int
	confirm *diskConfirm
	status  string
}

type diskMsg struct {
	seq  int
	view rpc.DiskView
	err  error
}

type diskDoneMsg struct {
	id   string
	item rpc.CleanupItem
	err  error
}

type diskShellMsg struct{ err error }

type layouter interface {
	ReviewLayout(ctx context.Context, open bool) error
}

func (m Model) openDisk() (tea.Model, tea.Cmd) {
	d := m.opts.Disk
	if d == nil {
		return m, nil
	}
	m.dk = diskState{open: true, sel: m.dk.sel}
	fetch := m.fetchDisk()
	return m, tea.Batch(layout(d, true), fetch)
}

func (m Model) closeDisk() (tea.Model, tea.Cmd) {
	m.dk = diskState{sel: m.dk.sel}
	return m, layout(m.opts.Disk, false)
}

func (m *Model) fetchDisk() tea.Cmd {
	m.dk.seq++
	m.dk.loading, m.dk.since = true, 0
	d, seq := m.opts.Disk, m.dk.seq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), diskCallTimeout)
		defer cancel()
		v, err := d.DiskView(ctx)
		return diskMsg{seq: seq, view: v, err: err}
	}
}

func (m *Model) gotDisk(msg diskMsg) {
	if !m.dk.open || msg.seq != m.dk.seq {
		return
	}
	m.dk.loading = false
	if msg.err != nil {
		m.dk.err = msg.err.Error()
		return
	}
	m.dk.err, m.dk.loaded, m.dk.view = "", true, msg.view
}

func (m *Model) gotDiskAction(msg diskDoneMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		m.dk.status = msg.err.Error()
	default:
		m.dk.status = msg.item.Outcome
		if msg.item.Outcome == "removed" || strings.HasSuffix(msg.item.Outcome, ", removed") {
			delete(m.worktrees, msg.id)
			m.rebuild()
		}
	}
	if !m.dk.open {
		return nil
	}
	return m.fetchDisk()
}

func (m *Model) tickDisk() tea.Cmd {
	if !m.dk.open {
		return nil
	}
	m.dk.since++
	_, pending := domain.TotalSize(m.diskRows())
	if depsPending(m.dk.view.DepsStore) {
		pending++
	}
	due := m.dk.since >= idleRefetchTicks || (pending > 0 && m.dk.since >= pendingRefetchTicks)
	if m.dk.loading || !due {
		return nil
	}
	return m.fetchDisk()
}

func depsPending(s *rpc.DepsStore) bool { return s != nil && s.Size == domain.SizePending }

func (m Model) diskRows() []domain.DiskRow {
	rows := make([]domain.DiskRow, 0, len(m.dk.view.Rows))
	for _, r := range m.dk.view.Rows {
		if _, ok := m.worktrees[r.WorktreeID]; ok {
			rows = append(rows, r)
		}
	}
	return rows
}

func (m Model) selectedDiskRow() (domain.DiskRow, bool) {
	rows := m.diskRows()
	for _, r := range rows {
		if r.WorktreeID == m.dk.sel {
			return r, true
		}
	}
	if len(rows) > 0 {
		return rows[0], true
	}
	return domain.DiskRow{}, false
}

func (m *Model) moveDisk(delta int) {
	rows := m.diskRows()
	cur := 0
	for i, r := range rows {
		if r.WorktreeID == m.dk.sel {
			cur = i
		}
	}
	if len(rows) > 0 {
		m.dk.sel = rows[min(max(cur+delta, 0), len(rows)-1)].WorktreeID
	}
}

func (m Model) diskKey(k string) (tea.Model, tea.Cmd) {
	if c := m.dk.confirm; c != nil {
		m.dk.confirm, m.dk.status = nil, ""
		if k == "y" {
			return m.confirmed(c)
		}
		return m, nil
	}
	m.dk.status = ""
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "w":
		return m.closeDisk()
	case "j", "down":
		m.moveDisk(1)
	case "up":
		m.moveDisk(-1)
	case "d":
		m.askRemove(false)
	case "b":
		m.askRemove(true)
	case "k":
		m.askKillPorts()
	case "o":
		return m, m.shellHere()
	case "g":
		return m.goToSession()
	}
	return m, nil
}

func (m *Model) askRemove(backup bool) {
	row, ok := m.selectedDiskRow()
	if !ok {
		return
	}
	label := worktreeLabel(m.worktrees[row.WorktreeID])
	switch {
	case row.Action == domain.CleanupKeep:
		m.dk.status = "can't remove: " + row.Reason
	case row.Action == domain.CleanupBackupThenAsk && !backup:
		m.dk.status = row.Reason + ": press b to back up and remove"
	case backup:
		m.dk.confirm = &diskConfirm{kind: diskBackupRemove, id: row.WorktreeID, label: label, prompt: "back up and remove " + label + "? y/n"}
	default:
		m.dk.confirm = &diskConfirm{kind: diskRemove, id: row.WorktreeID, label: label, prompt: "remove " + label + "? y/n"}
	}
}

func (m *Model) askKillPorts() {
	row, ok := m.selectedDiskRow()
	if !ok {
		return
	}
	ports := m.worktrees[row.WorktreeID].Ports
	if len(ports) == 0 || m.opts.Kill == nil {
		m.dk.status = "no dev servers in this worktree"
		return
	}
	label := portLabel(ports)
	m.dk.confirm = &diskConfirm{kind: diskKillPorts, id: row.WorktreeID, pgids: groupsOf(ports), label: label, prompt: "kill " + label + "? y/n"}
}

func (m Model) confirmed(c *diskConfirm) (tea.Model, tea.Cmd) {
	if c.kind == diskKillPorts {
		return m, m.kill(c.pgids)
	}
	w := m.worktrees[c.id]
	d, backup := m.opts.Disk, c.kind == diskBackupRemove
	m.dk.status = "removing " + c.label + "…"
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupCallTimeout)
		defer cancel()
		item, err := d.CleanupWorktree(ctx, w.Path, backup)
		return diskDoneMsg{id: c.id, item: item, err: err}
	}
}

func (m Model) shellHere() tea.Cmd {
	row, ok := m.selectedDiskRow()
	if !ok {
		return nil
	}
	d := m.opts.Disk
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return diskShellMsg{err: d.WorktreeShell(ctx, row.WorktreeID)}
	}
}

func (m Model) goToSession() (tea.Model, tea.Cmd) {
	row, ok := m.selectedDiskRow()
	if !ok {
		return m, nil
	}
	id := m.worktrees[row.WorktreeID].SessionID
	i := m.index(id)
	if i < 0 {
		m.dk.status = "no session owns this worktree"
		return m, nil
	}
	m.choose(i)
	next, _ := m.closeDisk()
	m = next.(Model)
	d, attend, focus := m.opts.Disk, m.opts.Attend, m.opts.Focus
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := d.ReviewLayout(ctx, false); err != nil {
			return errMsg{err}
		}
		if attend != nil {
			if err := attend.FocusSession(ctx, id); err != nil {
				return errMsg{err}
			}
		}
		if focus != nil {
			if err := focus.FocusMain(ctx); err != nil {
				return errMsg{err}
			}
		}
		return nil
	}
}

func formatBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	for i, unit := range units {
		v /= 1000
		if v < 1000 || i == len(units)-1 {
			if v < 10 {
				return fmt.Sprintf("%.1f %s", v, unit)
			}
			return fmt.Sprintf("%.0f %s", v, unit)
		}
	}
	return ""
}

func sizeText(n int64) string {
	if n == domain.SizePending {
		return "…"
	}
	return formatBytes(n)
}

func everyText(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

func totalText(n int64, pending int) string {
	if pending > 0 {
		return formatBytes(n) + "+"
	}
	return formatBytes(n)
}

func (m Model) diskScreen() string {
	lines, _ := m.diskScreenLines()
	return strings.Join(lines, "\n")
}

func (m Model) diskScreenLines() (lines, owners []string) {
	s := m.styles
	lines = []string{m.line(false, []piece{{s.header, " WORKTREES & DISK"}}, []piece{{s.sub, "esc back "}})}
	switch {
	case m.dk.err != "":
		lines = append(lines, "", m.line(false, []piece{{s.peach, " " + m.dk.err}}, nil))
	case !m.dk.loaded:
		lines = append(lines, "", m.line(false, []piece{{s.sub, " loading…"}}, nil))
	default:
		lines = append(lines, m.diskHeader()...)
		table, tableOwners := m.diskTable()
		owners = make([]string, len(lines), m.height)
		owners = append(owners, tableOwners...)
		lines = append(lines, table...)
	}
	footer := m.diskFooter()
	for len(lines)+len(footer) < m.height {
		lines = append(lines, "")
	}
	lines = append(lines, footer...)
	for len(owners) < len(lines) {
		owners = append(owners, "")
	}
	return lines, owners
}

type diskColumns struct{ task, branch int }

const (
	colState   = 16
	colRepo    = 9
	colPR      = 7
	colSession = 10
	colPort    = 12
	colSize    = 8
)

func (m Model) diskColumns() diskColumns {
	fixed := 1 + colState + colRepo + colPR + colSession + colPort + colSize + 2
	flex := max(m.width-fixed, 16)
	task := flex * 2 / 5
	return diskColumns{task: task, branch: flex - task}
}

func cell(s string, width int) string {
	s = ansi.Truncate(s, width-1, "…")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

func rightCell(s string, width int) string {
	return strings.Repeat(" ", max(width-ansi.StringWidth(s), 0)) + s
}

func (m Model) sessionNumber(id string) string {
	if i := m.index(id); i >= 0 {
		return fmt.Sprintf("%d %s", m.entries[i].num, m.entries[i].session.Harness)
	}
	return "–"
}

func (m Model) taskOf(w domain.Worktree) string {
	if x, ok := m.sessions[w.SessionID]; ok {
		if t, ok := m.tasks[x.TaskID]; ok {
			return taskLabel(t)
		}
	}
	return "–"
}

func (m Model) diskTable() ([]string, []string) {
	s := m.styles
	cols := m.diskColumns()
	head := []piece{
		{s.dim, " "}, {s.header, cell("STATE", colState)}, {s.header, cell("TASK", cols.task)}, {s.header, cell("REPO", colRepo)},
		{s.header, cell("BRANCH", cols.branch)}, {s.header, cell("PR", colPR)}, {s.header, cell("SESSION", colSession)},
		{s.header, cell("PORT", colPort)}, {s.header, rightCell("SIZE", colSize)},
	}
	out := []string{m.line(false, head, nil)}
	rows := m.diskRows()
	sel, selRow := m.selectedID(rows), 0
	var body, owners []string
	for _, r := range rows {
		w := m.worktrees[r.WorktreeID]
		isSel := r.WorktreeID == sel
		if isSel {
			selRow = len(body)
		}
		body = append(body, m.diskRowLine(r, w, cols, isSel))
		owners = append(owners, ownDisk+r.WorktreeID)
		if r.Action == domain.CleanupBackupThenAsk {
			body = append(body, m.line(isSel, []piece{{s.dim, "     ↳ "}, {s.peach, r.Reason}, {s.sub, " · b backup + remove · o shell · g session"}}, nil))
			owners = append(owners, ownDisk+r.WorktreeID)
		}
	}
	recent := m.recentLines()
	room := max(m.height-len(out)-len(recent)-8, 3)
	off := 0
	if selRow >= room {
		off = selRow - room + 2
	}
	off = min(off, max(len(body)-room, 0))
	body, owners = body[off:], owners[off:]
	if len(body) > room {
		body, owners = body[:room], owners[:room]
	}
	out = append(out, body...)
	owners = append([]string{""}, owners...)
	return append(append(out, ""), recent...), owners
}

func (m Model) selectedID(rows []domain.DiskRow) string {
	for _, r := range rows {
		if r.WorktreeID == m.dk.sel {
			return r.WorktreeID
		}
	}
	if len(rows) > 0 {
		return rows[0].WorktreeID
	}
	return ""
}

func (m Model) diskRowLine(r domain.DiskRow, w domain.Worktree, cols diskColumns, sel bool) string {
	s := m.styles
	bar := piece{s.text, " "}
	if sel {
		bar = piece{s.bar, "▌"}
	}
	var owner *domain.Session
	if x, ok := m.sessions[w.SessionID]; ok && w.SessionID != "" {
		owner = &x
	}
	status := domain.WorktreeStatus(w, owner, r.Action)
	state := piece{s.dim, cell(status, colState)}
	switch {
	case r.Action == domain.CleanupBackupThenAsk:
		state.st = s.need
	case strings.HasPrefix(status, "✓"):
		state.st = s.green
	case strings.HasSuffix(status, "in use"):
		state.st = s.blue
	}
	pr := "–"
	if w.PR != nil {
		pr = fmt.Sprintf("#%d", w.PR.Number)
	}
	ports := portLabel(w.Ports)
	sizeStyle := s.text
	if r.Size == domain.SizePending {
		sizeStyle = s.dim
	}
	return m.line(sel, []piece{
		bar, state, {s.text, cell(m.taskOf(w), cols.task)}, {s.sub, cell(repoName(w), colRepo)},
		{s.bold, cell(w.Branch, cols.branch)}, {s.blue, cell(pr, colPR)}, {s.text, cell(m.sessionNumber(w.SessionID), colSession)},
		{s.teal, cell(ports, colPort)}, {sizeStyle, rightCell(sizeText(r.Size), colSize)},
	}, nil)
}

func (m Model) recentLines() []string {
	s := m.styles
	out := []string{m.line(false, []piece{{s.header, " RECENTLY CLEANED"}}, nil)}
	recent := m.dk.view.Recent
	if len(recent) == 0 {
		return append(out, m.line(false, []piece{{s.dim, " nothing yet"}}, nil))
	}
	loc := m.opts.Now().Location()
	for _, r := range recent[:min(len(recent), recentShown)] {
		out = append(out, m.line(false, []piece{
			{s.dim, " " + r.At.In(loc).Format("15:04") + "  "},
			{s.bold, filepath.Base(r.Path) + "  "},
			{s.sub, cleanText(r.Outcome)},
		}, nil))
	}
	return out
}

func (m Model) diskFooter() []string {
	s := m.styles
	keys := m.line(false, []piece{
		{s.bold, " d"}, {s.sub, " remove  "}, {s.bold, "b"}, {s.sub, " backup+remove  "}, {s.bold, "o"}, {s.sub, " shell  "},
		{s.bold, "k"}, {s.sub, " kill port  "}, {s.bold, "g"}, {s.sub, " session  "}, {s.bold, "j/↑↓"}, {s.sub, " move"},
	}, nil)
	left := []piece{{s.badge, " WORKTREES "}, {s.sub, " w or esc closes"}}
	switch {
	case m.dk.confirm != nil:
		left = []piece{{s.badge, " WORKTREES "}, {s.peach, " " + m.dk.confirm.prompt}}
	case m.dk.status != "":
		left = []piece{{s.badge, " WORKTREES "}, {s.peach, " " + cleanText(m.dk.status)}}
	}
	return []string{"", keys, m.line(false, left, nil)}
}
