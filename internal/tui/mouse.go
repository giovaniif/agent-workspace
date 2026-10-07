package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const (
	ownSession      = "s:"
	ownHeader       = "h:"
	ownMenu         = "m:"
	ownPick         = "p:"
	ownResume       = "r:"
	ownField        = "f:"
	ownFieldColumns = "fc:"
	ownDisk         = "d:"
)

const (
	reviewWheelStep = 3
	listWheelStep   = 2
)

func LoadMouse(path string) (bool, error) {
	var cfg struct {
		UI struct {
			Mouse *bool `toml:"mouse"`
		} `toml:"ui"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, fmt.Errorf("tui: %s: %w", path, err)
	}
	return cfg.UI.Mouse == nil || *cfg.UI.Mouse, nil
}

func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.opts.NoMouse {
		return m, nil
	}
	ev := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		switch ev.Button {
		case tea.MouseWheelDown:
			return m.wheel(1)
		case tea.MouseWheelUp:
			return m.wheel(-1)
		}
	case tea.MouseClickMsg:
		switch {
		case m.menu != nil:
			return m.menuClick(ev.X, ev.Y)
		case ev.Button == tea.MouseLeft:
			return m.click(ev.X, ev.Y)
		case ev.Button == tea.MouseRight:
			return m.rightClick(ev.Y)
		}
	case tea.MouseMotionMsg:
		if m.rv.open && m.rv.dragging {
			m.dragTo(ev.Y)
		}
	case tea.MouseReleaseMsg:
		m.rv.dragging = false
	}
	return m, nil
}

func (m Model) wheel(delta int) (tea.Model, tea.Cmd) {
	switch {
	case m.ob != nil, m.dialog != nil, m.launching != nil, m.renaming != nil, m.help:
	case m.rv.open:
		if !m.rv.typing {
			m.moveLine(delta * reviewWheelStep)
		}
	case m.dk.open:
		m.moveDisk(delta)
	case m.picker != nil:
		p := *m.picker
		p.cursor = min(max(p.cursor+delta, 0), len(p.choices)-1)
		m.picker = &p
	default:
		_, off := m.listGeometry()
		m.scroll, m.scrolled = off+delta*listWheelStep, true
		_, m.scroll = m.listGeometry()
	}
	return m, nil
}

func (m *Model) selectInPlace(id string) {
	owners, off := m.listGeometry()
	before := slices.Index(owners, ownSession+id) - off
	m.choose(m.index(id))
	owners, _ = m.listGeometry()
	m.scroll, m.scrolled = slices.Index(owners, ownSession+id)-before, true
	_, m.scroll = m.listGeometry()
}

func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	var lines, owners []string
	margin := 0
	switch {
	case m.ob != nil:
		lines = strings.Split(m.setupScreen(), "\n")
	case m.opts.NewSessionOnly:
		if m.dialog == nil {
			return m, nil
		}
		lines, owners, margin = m.dialogScreenLines()
	case m.rv.open:
		if next, cmd, ok := m.reviewClick(x, y); ok {
			return next, cmd
		}
		lines = strings.Split(m.reviewView(), "\n")
	case m.dk.open:
		lines, owners = m.diskScreenLines()
	default:
		lines, owners = m.mainScreen()
	}
	if y < 0 || y >= len(lines) {
		return m, nil
	}
	if y < len(owners) && owners[y] != "" {
		return m.clickOwner(owners[y], x-margin)
	}
	if !m.hintRow(lines, y) {
		return m, nil
	}
	plain := ansi.Strip(lines[y])
	if m.help && !m.rv.open && !m.dk.open {
		x = len([]rune(plain)) - len([]rune(strings.TrimLeft(plain, " ")))
	}
	k, ok := hintAt(plain, x)
	if !ok || (m.typing() && printable(k)) {
		return m, nil
	}
	return m.Update(k)
}

func (m Model) clickOwner(owner string, x int) (tea.Model, tea.Cmd) {
	switch {
	case strings.HasPrefix(owner, ownSession), strings.HasPrefix(owner, ownHeader):
		m.selectInPlace(owner[len(ownSession):])
		return m, m.focus()
	case strings.HasPrefix(owner, ownPick):
		i, _ := strconv.Atoi(strings.TrimPrefix(owner, ownPick))
		shown := m.picker.shown()
		if i < 0 || i >= len(shown) {
			return m, nil
		}
		return m.applyChoice(*m.picker, shown[i])
	case strings.HasPrefix(owner, ownResume):
		i, _ := strconv.Atoi(strings.TrimPrefix(owner, ownResume))
		if m.resuming == nil || i < 0 || i >= len(m.resuming.choices) {
			return m, nil
		}
		return m.resume(m.resuming.choices[i].ID)
	case strings.HasPrefix(owner, ownFieldColumns):
		w, _ := strconv.Atoi(strings.TrimPrefix(owner, ownFieldColumns))
		col := min(max(x, 0)/max(w, 1), int(fieldEffort-fieldHarness))
		m.own().field = fieldHarness + field(col)
	case strings.HasPrefix(owner, ownField):
		f, _ := strconv.Atoi(strings.TrimPrefix(owner, ownField))
		m.own().field = field(f)
	case strings.HasPrefix(owner, ownDisk):
		m.dk.sel, m.dk.status = strings.TrimPrefix(owner, ownDisk), ""
	}
	return m, nil
}

func (m Model) hintRow(lines []string, y int) bool {
	if m.help && !m.rv.open && !m.dk.open {
		return true
	}
	plain := ansi.Strip(lines[y])
	if strings.Contains(plain, "esc ") || strings.HasSuffix(strings.TrimSpace(plain), "esc") {
		return true
	}
	top := len(lines)
	for top > 0 && strings.TrimSpace(ansi.Strip(lines[top-1])) != "" {
		top--
	}
	return y >= top
}

func (m Model) typing() bool {
	return m.dialog != nil || m.launching != nil || m.renaming != nil || m.rv.typing
}

func printable(k tea.KeyPressMsg) bool { return k.Text != "" && k.Text != " " }

var namedKeys = map[string]tea.KeyPressMsg{
	"enter": {Code: tea.KeyEnter},
	"⏎":     {Code: tea.KeyEnter},
	"esc":   {Code: tea.KeyEscape},
	"tab":   {Code: tea.KeyTab},
	"⇥":     {Code: tea.KeyTab},
	"space": {Code: tea.KeySpace, Text: " "},
	"␣":     {Code: tea.KeySpace, Text: " "},
	"←":     {Code: tea.KeyLeft},
	"→":     {Code: tea.KeyRight},
	"↑":     {Code: tea.KeyUp},
	"↓":     {Code: tea.KeyDown},
}

func hintAt(line string, x int) (tea.KeyPressMsg, bool) {
	cells := []rune(strings.ReplaceAll(line, " · ", "   "))
	if x < 0 || x >= len(cells) || cells[x] == ' ' && gapAt(cells, x) {
		return tea.KeyPressMsg{}, false
	}
	start := x
	for start > 0 && !gapAt(cells, start-1) {
		start--
	}
	for start < len(cells) && cells[start] == ' ' {
		start++
	}
	end := start
	for end < len(cells) && cells[end] != ' ' {
		end++
	}
	token := string(cells[start:end])
	if word := wordAt(cells, x); word == "y/n" {
		token = word
	}
	if parts := strings.Split(token, "/"); len(parts) > 1 && len([]rune(token)) > 1 {
		token = parts[0]
		if x >= start && x < end {
			at := 0
			for _, p := range parts {
				if x-start < at+len([]rune(p))+1 {
					token = p
					break
				}
				at += len([]rune(p)) + 1
			}
		}
		if word := wordAt(cells, x); word == "y/n" {
			token = map[bool]string{true: "y", false: "n"}[cells[x] != 'n']
		}
	}
	return keyFor(token)
}

func gapAt(cells []rune, i int) bool {
	if cells[i] != ' ' {
		return false
	}
	return (i > 0 && cells[i-1] == ' ') || (i+1 < len(cells) && cells[i+1] == ' ')
}

func wordAt(cells []rune, x int) string {
	if cells[x] == ' ' {
		return ""
	}
	s, e := x, x
	for s > 0 && cells[s-1] != ' ' {
		s--
	}
	for e < len(cells) && cells[e] != ' ' {
		e++
	}
	return string(cells[s:e])
}

func keyFor(token string) (tea.KeyPressMsg, bool) {
	if k, ok := namedKeys[token]; ok {
		return k, true
	}
	if rest, ok := strings.CutPrefix(token, "ctrl+"); ok && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}, true
	}
	if rest, ok := strings.CutPrefix(token, "^"); ok && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}, true
	}
	r := []rune(token)
	if len(r) != 1 || r[0] > unicode.MaxASCII || !unicode.IsPrint(r[0]) || unicode.IsDigit(r[0]) || r[0] == ' ' {
		return tea.KeyPressMsg{}, false
	}
	return tea.KeyPressMsg{Code: r[0], Text: token}, true
}

func (m Model) reviewClick(x, y int) (Model, tea.Cmd, bool) {
	if m.rv.typing {
		return m, nil, false
	}
	_, treeW, _ := m.reviewWidths()
	left := railWidth + 1
	if w := m.agentColumnWidth(); w > 0 {
		left += w + 1
	}
	ax := x - left
	if ax < 0 {
		return m, nil, false
	}
	switch y {
	case 1:
		labels, _ := m.scopeChips()
		if i := chipAt(scopeLead, labels, ax); i >= 0 {
			m.rv.scope = domain.ReviewScopes[i]
			fetch := m.fetchReview()
			return m, fetch, true
		}
		return m, nil, true
	case 2:
		labels, ids, _ := m.worktreeChips()
		if i := chipAt(worktreeLead, labels, ax); i >= 0 {
			m.rv.worktree = ids[i]
			fetch := m.fetchReview()
			return m, fetch, true
		}
		return m, nil, true
	}
	bodyH := max(m.height-reviewChrome, 1)
	row := y - (reviewChrome - 1)
	if row < 0 || row >= bodyH {
		return m, nil, false
	}
	if ax < treeW {
		_, files := m.treeLines(treeW, bodyH)
		if f := files[row]; f >= 0 && f != m.rv.cur {
			m.rv.cur, m.rv.scroll = f, 0
			m.resetLine()
		}
		return m, nil, true
	}
	f, ok := m.rv.current()
	if ax == treeW || !ok || row == 0 {
		return m, nil, true
	}
	if i := m.rv.scroll + row - 1; i < len(m.rv.rows(f)) {
		m.rv.line, m.rv.marking, m.rv.dragging = i, false, true
		m.clampLine()
	}
	return m, nil, true
}

func (m *Model) dragTo(y int) {
	f, ok := m.rv.current()
	if !ok {
		return
	}
	i := m.rv.scroll + y - reviewChrome
	if i < 0 || i >= len(m.rv.rows(f)) || i == m.rv.line {
		return
	}
	if !m.rv.marking {
		m.rv.marking, m.rv.mark = true, m.rv.line
	}
	m.rv.line = i
	m.clampLine()
}

func chipAt(lead string, labels []string, x int) int {
	at := ansi.StringWidth(lead)
	for i, l := range labels {
		w := ansi.StringWidth(l) + 2
		if x >= at && x < at+w {
			return i
		}
		at += w + 1
	}
	return -1
}
