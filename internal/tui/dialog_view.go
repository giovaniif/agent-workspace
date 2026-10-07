package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const dialogMaxWidth = 96

const dialogColumnsFrom = 72

func (m Model) dialogScreen() string {
	lines, _, _ := m.dialogScreenLines()
	return strings.Join(lines, "\n")
}

func (m Model) dialogScreenLines() (lines, owners []string, margin int) {
	f := m
	f.width = max(min(m.width-4, dialogMaxWidth), 1)
	lines, keep, owners := f.dialogLines()
	formLen := len(lines)
	formRow, formCol := 0, 0
	if m.dialog != nil {
		formRow, formCol = m.dialog.menuAt, m.dialog.menuCol
	}
	for len(owners) < len(lines) {
		owners = append(owners, "")
	}
	lines = dialogViewport(lines, keep, m.height)
	owners = dialogViewport(owners, keep, m.height)
	row, ok := menuScreenRow(formRow, formLen, keep, m.height)
	margin = max((m.width-f.width)/2, 0)
	span := margin + f.width
	pad := strings.Repeat(" ", margin)
	for i, l := range lines {
		if gap := f.width - ansi.StringWidth(l); gap > 0 {
			l += strings.Repeat(" ", gap)
		}
		lines[i] = pad + l
	}
	lead := 0
	if len(lines) < m.height {
		lead = 1
		blank := strings.Repeat(" ", span)
		lines = append([]string{blank}, lines...)
		owners = append([]string{""}, owners...)
		for len(lines) < m.height {
			lines = append(lines, blank)
			owners = append(owners, "")
		}
	}
	if ok {
		lines, owners = m.overlayMenu(lines, owners, row+lead, margin+formCol)
	}
	return lines, owners, margin
}

func dialogWindow(n, keep, height int) (start int, clipped bool) {
	if height <= 0 || n <= height {
		return 0, false
	}
	if height < 3 {
		return n - height, true
	}
	room := height - 2
	body := n - 2
	return min(max(keep-1-room/2, 0), max(body-room, 0)), true
}

func dialogViewport[T any](lines []T, keep, height int) []T {
	start, clipped := dialogWindow(len(lines), keep, height)
	if !clipped {
		return lines
	}
	if height < 3 {
		return lines[start:]
	}
	room := height - 2
	title, body, actions := lines[0], lines[1:len(lines)-1], lines[len(lines)-1]
	return append(append([]T{title}, body[start:start+room]...), actions)
}

func menuScreenRow(formRow, formLen, keep, height int) (int, bool) {
	if formRow < 0 || formRow >= formLen {
		return 0, false
	}
	start, clipped := dialogWindow(formLen, keep, height)
	if !clipped {
		return formRow, true
	}
	if height < 3 {
		return formRow - start, true
	}
	if formRow == 0 {
		return 0, true
	}
	if formRow == formLen-1 {
		return height - 1, true
	}
	room := height - 2
	body := formRow - 1
	if body < start || body >= start+room {
		return 0, false
	}
	return 1 + body - start, true
}

func (m Model) dialogLines() ([]string, int, []string) {
	s := m.styles
	d := m.dialog
	w := m.width
	var out, owners []string
	keep := 0
	add := func(f field, lines ...string) {
		if d.field == f {
			keep = len(out)
		}
		for len(owners) < len(out) {
			owners = append(owners, "")
		}
		out = append(out, lines...)
		for range lines {
			owners = append(owners, ownField+strconv.Itoa(int(f)))
		}
	}
	out = append(out, m.titleBar(" New session", "esc "), "")

	item := []piece{{s.text, d.workItem}}
	if d.field == fieldWorkItem {
		item = append(item, piece{s.bar, "▏"})
	} else if d.workItem == "" {
		item = []piece{{s.dim, "a Linear issue, a GitHub PR or a few words"}}
	}
	add(fieldWorkItem, append([]string{m.label(fieldWorkItem, "Work item")}, m.box(w, item, d.field == fieldWorkItem)...)...)
	out = append(out, m.line(false, []piece{{s.sub, " " + workItemPreview(d.workItem)}}, nil), "")

	ws := []piece{{s.dim, "none: type a folder's path"}}
	facts := ""
	if len(d.spaces) > 0 {
		space := d.spaces[d.ws]
		ws = []piece{{s.bold, "‹ " + filepath.Base(space.Root) + " ›"}, {s.sub, "  " + workspaceKind(space)}}
		facts = space.Root
		if space.Root == d.last {
			facts += " · last used"
		}
	}
	if d.path != "" {
		ws = []piece{{s.text, d.path}}
		if d.field == fieldWorkspace {
			ws = append(ws, piece{s.bar, "▏"})
		}
		facts = d.typed().Path + " · " + m.folderLabel()
	}
	add(fieldWorkspace, append([]string{m.label(fieldWorkspace, "Workspace")}, m.box(w, ws, d.field == fieldWorkspace)...)...)
	if d.field == fieldWorkspace {
		add(fieldWorkspace, m.dropdown()...)
	}
	out = append(out, m.line(false, []piece{{s.sub, " " + facts}}, nil), "")

	for len(owners) < len(out) {
		owners = append(owners, "")
	}
	origin := len(out)
	pick := m.pickers(&keep, len(out))
	out = append(out, pick...)
	owners = append(owners, m.pickerOwners(len(pick))...)
	menuRow, menuCol := m.modelMenuOrigin()
	d.menuAt = origin + menuRow + 1
	d.menuCol = menuCol
	out = append(out, "")
	out = append(out, m.startsAt()...)
	if lines := m.adviceBox(); len(lines) > 0 {
		out = append(append(out, ""), lines...)
	}
	out = append(out, "")
	switch {
	case d.busy && !d.started:
		keep = len(out)
		out = append(out, m.line(false, []piece{{s.sub, " starting…"}}, nil))
	case d.err != "":
		keep = len(out)
		out = append(out, m.line(false, []piece{{s.peach, " ✗ " + d.err}}, nil))
	}
	hint := []piece{{s.dim, " ⇥ next · ←/→ change"}}
	switch {
	case d.field == fieldWorkspace && d.path != "":
		hint = []piece{{s.dim, " ↑/↓ pick · → open · ← up · ⇥ next"}}
	case d.field == fieldWorkspace:
		hint = []piece{{s.dim, " ⇥ next · ←/→ change · type ./ ../ ~/ for a folder"}}
	}
	buttons := []piece{{s.sub, "esc cancel"}, {s.text, "  "}, {s.badge, " ⏎ create "}, {s.text, " "}}
	out = append(out, m.line(false, hint, buttons))
	for i, l := range out {
		if gap := w - ansi.StringWidth(l); gap > 0 {
			out[i] += strings.Repeat(" ", gap)
		}
	}
	return out, keep, owners
}

func (m Model) pickerOwners(n int) []string {
	out := make([]string, n)
	for i := range out {
		if m.width < dialogColumnsFrom {
			out[i] = ownField + strconv.Itoa(int(fieldHarness)+i/2)
		} else {
			out[i] = ownFieldColumns + strconv.Itoa((m.width-1)/3)
		}
	}
	return out
}

func (m Model) titleBar(left, right string) string {
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right), 1)
	return m.styles.badge.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) label(f field, name string) string {
	st := m.styles.bold
	if m.dialog.field == f {
		st = m.styles.brand
	}
	return m.line(false, []piece{{st, " " + name}}, nil)
}

func (m Model) box(w int, content []piece, active bool) []string {
	border := m.styles.dim
	if active {
		border = m.styles.bar
	}
	span := max(w, 4)
	textW := span - 4
	var used int
	var fitted []piece
	for _, p := range content {
		pw := ansi.StringWidth(p.s)
		if used+pw > textW {
			fitted = append(fitted, piece{p.st, ansi.Truncate(p.s, max(textW-used, 0), "…")})
			used = textW
			break
		}
		fitted = append(fitted, p)
		used += pw
	}
	mid := border.Render(" │ ")
	for _, p := range fitted {
		mid += p.st.Render(p.s)
	}
	mid += strings.Repeat(" ", max(textW-used, 0)) + border.Render("│")
	rule := span - 3
	return []string{
		border.Render(" ╭" + strings.Repeat("─", rule) + "╮"),
		mid,
		border.Render(" ╰" + strings.Repeat("─", rule) + "╯"),
	}
}

func (m Model) pickers(keep *int, at int) []string {
	s := m.styles
	d := m.dialog
	harness := make([]piece, 0, 2*len(harnessChoices))
	for i, h := range harnessChoices {
		if i == d.harness {
			harness = append(harness, piece{s.brand, "● " + h})
		} else {
			harness = append(harness, piece{s.dim, "○ " + h})
		}
		harness = append(harness, piece{s.text, "  "})
	}
	modelValue := m.modelPieces()
	effort := d.efforts[d.effort]
	if effort == "" {
		effort = defaultLabel(m.defaultEffort())
		if m.chosenHarness() == domain.HarnessOmp {
			effort = "Default"
		}
	}
	cols := []struct {
		f     field
		name  string
		value []piece
	}{
		{fieldHarness, "Harness", harness},
		{fieldModel, "Model", modelValue},
		{fieldEffort, "Effort", []piece{{s.bold, "‹ " + effort + " ›"}}},
	}
	hw, mw, wide := m.pickerWidths()
	if !wide {
		var out []string
		for _, c := range cols {
			if d.field == c.f {
				*keep = at + len(out)
			}
			out = append(out, m.label(c.f, c.name), m.line(false, append([]piece{{s.text, " "}}, c.value...), nil))
		}
		return out
	}
	widths := []int{hw, mw, m.width - 1 - hw - mw}
	var labels, values []piece
	for i, c := range cols {
		st := s.bold
		if d.field == c.f {
			st, *keep = s.brand, at
		}
		labels = append(labels, padTo(widths[i], piece{st, " " + c.name})...)
		values = append(values, padTo(widths[i], append([]piece{{s.text, " "}}, c.value...)...)...)
	}
	return []string{m.line(false, labels, nil), m.line(false, values, nil)}
}

func (m Model) pickerWidths() (harnessW, modelW int, wide bool) {
	if m.width < dialogColumnsFrom || m.dialog == nil {
		return 0, 0, false
	}
	hw := 1
	for i, h := range harnessChoices {
		label := "○ " + h + "  "
		if i == m.dialog.harness {
			label = "● " + h + "  "
		}
		hw += ansi.StringWidth(label)
	}
	rest := m.width - 1 - hw
	if rest < 16 {
		return 0, 0, false
	}
	return hw, rest / 2, true
}

func padTo(width int, ps ...piece) []piece {
	var out []piece
	used := 0
	for _, p := range ps {
		if used >= width {
			break
		}
		pw := ansi.StringWidth(p.s)
		if used+pw > width {
			out = append(out, piece{p.st, ansi.Truncate(p.s, width-used, "…")})
			used = width
			break
		}
		out = append(out, p)
		used += pw
	}
	if used < width {
		out = append(out, piece{lipgloss.NewStyle(), strings.Repeat(" ", width-used)})
	}
	return out
}

func (m Model) modelPieces() []piece {
	d := m.dialog
	s := m.styles
	if d.typing {
		shown := d.model
		ps := []piece{{s.bold, shown}}
		if d.field == fieldModel {
			ps = append(ps, piece{s.text, "▏"})
		}
		return ps
	}
	label := d.model
	if label == "" {
		label = "Default"
	}
	return []piece{{s.bold, "‹ " + label + " ›"}}
}

func (m Model) modelMenuOrigin() (row, col int) {
	hw, _, wide := m.pickerWidths()
	if !wide {
		return 3, 1
	}
	return 1, hw + 1
}

func placeMenu(row, box, height int) int {
	if box > height {
		box = height
	}
	if row < 0 {
		row = 0
	}
	if row+box <= height {
		return row
	}
	above := row - 1 - box
	if above >= 0 {
		return above
	}
	return max(height-box, 0)
}

func (m Model) overlayMenu(lines, owners []string, row, col int) ([]string, []string) {
	d := m.dialog
	if d == nil || row < 0 {
		return lines, owners
	}
	shown, selected := d.menuRows()
	if len(shown) == 0 {
		return lines, owners
	}
	width := 0
	for _, l := range lines {
		width = max(width, ansi.StringWidth(l))
	}
	if width == 0 {
		width = max(m.width, 1)
	}
	for len(lines) < max(m.height, 0) {
		lines = append(lines, strings.Repeat(" ", width))
	}
	for len(owners) < len(lines) {
		owners = append(owners, "")
	}
	box := m.menuBox(shown, selected, m.menuInner(max(width-col, 1)))
	row = placeMenu(row, len(box), len(lines))
	mark := ownField + strconv.Itoa(int(fieldModel))
	for i, b := range box {
		at := row + i
		if at < 0 || at >= len(lines) {
			continue
		}
		painted := paintOver(lines[at], b, col)
		if gap := width - ansi.StringWidth(painted); gap > 0 {
			painted += strings.Repeat(" ", gap)
		}
		lines[at] = painted
		owners[at] = mark
	}
	return lines, owners
}

func paintOver(line, patch string, col int) string {
	width := ansi.StringWidth(line)
	pw := ansi.StringWidth(patch)
	if col > width {
		line += strings.Repeat(" ", col-width)
		width = col
	}
	left := ansi.Truncate(line, col, "")
	right := ""
	if end := col + pw; end < width {
		right = ansi.Cut(line, end, width)
	}
	return left + patch + right
}

func (m Model) menuInner(room int) int {
	inner := 8
	if m.dialog != nil {
		for _, id := range m.dialog.modelLists[m.dialog.picked()] {
			inner = max(inner, ansi.StringWidth(id)+2)
		}
	}
	return min(inner, max(room-2, 1))
}

func (m Model) menuBox(shown []string, selected, inner int) []string {
	t := m.opts.Theme
	edge := m.styles.dim
	text := m.styles.text
	hot := m.styles.bold.Background(lipgloss.Color(t.Selected))
	rule := func(left, fill, right string) string {
		return edge.Render(left + strings.Repeat(fill, inner) + right)
	}
	out := []string{rule("╭", "─", "╮")}
	for i, id := range shown {
		mark := "  "
		body := text
		if i == selected {
			mark = "▸ "
			body = hot
		}
		label := mark + id
		if ansi.StringWidth(label) > inner {
			label = ansi.Truncate(label, inner, "…")
		}
		label += strings.Repeat(" ", inner-ansi.StringWidth(label))
		if i == selected {
			out = append(out, body.Render("│"+label+"│"))
			continue
		}
		out = append(out, edge.Render("│")+body.Render(label)+edge.Render("│"))
	}
	return append(out, rule("╰", "─", "╯"))
}

func workspaceKind(w domain.Workspace) string {
	if w.Kind == "" {
		return "new · added when you create"
	}
	if w.Kind == domain.WorkspaceSingle {
		return "single repo"
	}
	return fmt.Sprintf("orchestration root · %d repos", len(w.Repos))
}

func workItemPreview(input string) string {
	if strings.TrimSpace(input) == "" {
		return "the session is named after it"
	}
	t := domain.ParseWorkItem(input)
	switch t.Source {
	case domain.TaskLinear:
		title := t.IssueTitle
		if title == "" {
			title = "title from Linear"
		}
		return t.Ref + " · " + title + " · session will be named after it"
	case domain.TaskPR:
		return t.Ref + " · session will be named after the PR"
	}
	return "text · session named “" + t.Text + "”"
}

const dropdownRows = 6

func (m Model) dropdown() []string {
	s := m.styles
	d := m.dialog
	ms := d.matches()
	start := min(max(d.pick-dropdownRows/2, 0), max(len(ms)-dropdownRows, 0))
	var out []string
	for i := start; i < min(start+dropdownRows, len(ms)); i++ {
		c := ms[i]
		row := []piece{{s.dim, "     "}, {s.text, c.Name + "/"}}
		if i == d.pick {
			row = []piece{{s.brand, "   ▸ "}, {s.brand, c.Name + "/"}}
		}
		switch c.Git {
		case domain.GitDir:
			row = append(row, piece{s.sub, "  repo"})
		case domain.GitFile:
			row = append(row, piece{s.sub, "  worktree"})
		}
		out = append(out, m.line(false, row, nil))
	}
	if more := len(ms) - start - dropdownRows; more > 0 {
		out = append(out, m.line(false, []piece{{s.dim, fmt.Sprintf("     +%d more", more)}}, nil))
	}
	return out
}

func (m Model) folderLabel() string {
	d := m.dialog
	if space, _ := d.chosen(); space.Kind != "" {
		return workspaceKind(space)
	}
	switch d.folder() {
	case folderFound:
		return workspaceKind(domain.Workspace{})
	case folderMissing:
		return "no such folder"
	}
	return "…"
}

func (m Model) startsAt() []string {
	s := m.styles
	d := m.dialog
	space, ok := d.chosen()
	if !ok {
		return nil
	}
	if space.Kind == "" {
		bar := piece{s.dim, " ▌ "}
		return []string{
			m.line(false, []piece{bar, {s.bold, "Starts in " + filepath.Base(space.Root)}}, nil),
			m.line(false, []piece{bar, {s.sub, "agentws checks whether it is one repo or a folder of repos when you create"}}, nil),
		}
	}
	plan := domain.PlanSessionStart(space, domain.TaskSlug(domain.ParseWorkItem(d.workItem)), "", nil)
	title := "Starts at the " + filepath.Base(space.Root) + " root"
	detail := "The agent creates worktrees as it needs them; each one attaches to this session."
	if wt := plan.Worktree; wt != nil {
		title = "Starts in a fresh worktree"
		name := wt.Repo
		if strings.TrimSpace(d.workItem) != "" {
			name += ":" + wt.Branch
		}
		detail = name + " from " + wt.Base
	}
	bar := piece{s.dim, " ▌ "}
	return []string{
		m.line(false, []piece{bar, {s.bold, title}}, nil),
		m.line(false, []piece{bar, {s.sub, detail}}, nil),
	}
}

func (m Model) adviceBox() []string {
	advice, ok := m.advice()
	if !ok {
		return nil
	}
	s := m.styles
	low := advice.Low
	head := fmt.Sprintf("%s %s window %d%% used", harnessName(low.Harness), domain.WindowLabel(low.Window), usedPercent(low))
	if reset := resetClock(low.ResetsAt, m.opts.Now()); reset != "" {
		head += " · resets " + reset
	}
	bar := piece{s.peach, " ▌ "}
	var action []piece
	detail := "No " + harnessName(advice.Other) + " figures yet"
	if offer, ok := m.fallbackOffer(); ok {
		action = []piece{{s.need, "ctrl+s start in " + string(offer.Request.Harness) + " "}}
		detail = fmt.Sprintf("%s %s is %d%% used", harnessName(offer.Request.Harness), domain.WindowLabel(offer.Advice.OtherShortest.Window), usedPercent(*offer.Advice.OtherShortest))
		if offer.Request.Model != "" {
			detail += " · starts as " + offer.Request.Model
		}
	} else if o := advice.OtherShortest; o != nil {
		detail = fmt.Sprintf("%s %s is %d%% used", harnessName(advice.Other), domain.WindowLabel(o.Window), usedPercent(*o))
		if advice.Other != domain.HarnessCodex {
			action = []piece{{s.need, "ctrl+s start in " + string(advice.Other) + " "}}
		}
	}
	if m.width < dialogColumnsFrom {
		out := []string{
			m.line(false, []piece{bar, {s.need, "! " + head}}, nil),
			m.line(false, []piece{bar, {s.sub, "  " + detail}}, nil),
		}
		if len(action) > 0 {
			out = append(out, m.line(false, append([]piece{bar, {s.text, "  "}}, action...), nil))
		}
		return out
	}
	return []string{
		m.line(false, []piece{bar, {s.need, "! " + head}}, action),
		m.line(false, []piece{bar, {s.sub, "  " + detail}}, nil),
	}
}

func harnessName(h domain.Harness) string {
	if h == domain.HarnessClaude {
		return "Claude"
	}
	return cmp.Or(domain.Spec(h).Name, string(h))
}

func defaultLabel(name string) string {
	if name == "" {
		return "default"
	}
	return "default · " + name
}

func (m Model) defaultModel() string {
	h := m.chosenHarness()
	if name := m.opts.HarnessDefaults[h].Model; name != "" {
		return name
	}
	var name, id string
	var at time.Time
	for _, s := range m.sessions {
		if s.Harness == h && s.Model != "" && (name == "" || s.LimitsAt.After(at) || s.LimitsAt.Equal(at) && s.ID > id) {
			name, at, id = s.Model, s.LimitsAt, s.ID
		}
	}
	return name
}

func (m Model) defaultEffort() string {
	d := m.opts.HarnessDefaults[m.chosenHarness()]
	if d.Effort != "" {
		return d.Effort
	}
	return d.EffortByModel[m.defaultModel()]
}
