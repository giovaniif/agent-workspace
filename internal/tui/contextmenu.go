package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const sessionMenuCol = 3

type sessionMenu struct {
	session string
	cursor  int
}

type menuAction struct {
	label string
	key   tea.KeyPressMsg
}

var sessionMenuActions = []menuAction{
	{label: "End session", key: tea.KeyPressMsg{Code: 'x', Text: "x"}},
}

func (m Model) menuOpenable() bool {
	return m.ob == nil && !m.opts.NewSessionOnly && !m.rv.open && !m.dk.open && m.dialog == nil &&
		m.launching == nil && m.renaming == nil && m.picker == nil && m.resuming == nil && !m.help &&
		m.confirm == nil && m.ending == ""
}

func (m Model) rightClick(y int) (tea.Model, tea.Cmd) {
	if !m.menuOpenable() {
		return m, nil
	}
	_, owners := m.mainScreen()
	if y < 0 || y >= len(owners) || !strings.HasPrefix(owners[y], ownSession) {
		return m, nil
	}
	id := strings.TrimPrefix(owners[y], ownSession)
	m.selectInPlace(id)
	m.menu = &sessionMenu{session: id}
	return m, nil
}

func (m Model) menuClick(x, y int) (tea.Model, tea.Cmd) {
	_, owners := m.mainScreen()
	menu := m.menu
	m.menu = nil
	if y < 0 || y >= len(owners) || !strings.HasPrefix(owners[y], ownMenu) || x < sessionMenuCol {
		return m, nil
	}
	i, _ := strconv.Atoi(strings.TrimPrefix(owners[y], ownMenu))
	return m.runMenuAction(menu.session, i)
}

func (m Model) menuKey(k string) (tea.Model, tea.Cmd) {
	menu := *m.menu
	switch k {
	case "up", "k":
		menu.cursor = max(menu.cursor-1, 0)
	case "down", "j":
		menu.cursor = min(menu.cursor+1, len(sessionMenuActions)-1)
	case "enter":
		m.menu = nil
		return m.runMenuAction(menu.session, menu.cursor)
	default:
		m.menu = nil
		return m, nil
	}
	m.menu = &menu
	return m, nil
}

func (m Model) runMenuAction(session string, i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(sessionMenuActions) || session != m.selected {
		return m, nil
	}
	return m.key(sessionMenuActions[i].key)
}

func (m *Model) closeStaleMenu() {
	if m.menu != nil && m.menu.session != m.selected {
		m.menu = nil
	}
}

func (m Model) overlaySessionMenu(lines, owners []string) ([]string, []string) {
	anchor := slices.Index(owners, ownSession+m.menu.session)
	if anchor < 0 {
		return lines, owners
	}
	labels := make([]string, len(sessionMenuActions))
	inner := 0
	for i, a := range sessionMenuActions {
		labels[i] = a.label
		inner = max(inner, ansi.StringWidth(a.label)+3)
	}
	box := m.menuBox(labels, m.menu.cursor, inner)
	row := placeMenu(anchor+1, len(box), len(lines))
	for i, b := range box {
		at := row + i
		if at < 0 || at >= len(lines) {
			continue
		}
		lines[at] = paintOver(lines[at], b, sessionMenuCol)
		owners[at] = ""
		if i > 0 && i < len(box)-1 {
			owners[at] = ownMenu + strconv.Itoa(i-1)
		}
	}
	return lines, owners
}
