package tui

import (
	"cmp"
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

// why: covers the setup recipe, which may install dependencies.
const startTimeout = 15 * time.Minute

const callTimeout = 5 * time.Second

type field int

const (
	fieldWorkItem field = iota
	fieldWorkspace
	fieldHarness
	fieldModel
	fieldEffort
	fieldCount
)

var harnessChoices = catalogNames()

func effortsFor(h domain.Harness) []string {
	return append([]string{""}, domain.SwitchChoices(h, domain.SwitchEffort)...)
}

func catalogNames() []string {
	var out []string
	for _, h := range domain.Harnesses() {
		out = append(out, string(h))
	}
	return out
}

type dialog struct {
	field    field
	workItem string
	model    string
	spaces   []domain.Workspace
	last     string
	ws       int
	harness  int
	effort   int
	// why: holds any mapped effort a fallback brought that the picker lacks.
	efforts []string
	// why: remembers the Claude start a fallback replaced, so going back restores it.
	fallback *fallbackTrace
	err      string
	busy     bool
	started  bool
	// why: tells this dialog's start reply from one sent by a dialog closed earlier.
	seq        int
	defaults   map[domain.Harness]Defaults
	modelLists map[domain.Harness][]string
	// why: ←/→ keep cycling, so a typed omp query remembers the cycled value and ctrl-e can undo a match.
	typing  bool
	menu    bool
	query   string
	saved   string
	suggest int
	// why: the menu is painted on the finished screen, after the form has been scrolled and centered.
	menuAt, menuCol int
}

type fallbackTrace struct {
	fromModel, model   string
	fromEffort, effort string
}

type sessionStartedMsg struct {
	seq     int
	session domain.Session
}

type startFailedMsg struct {
	seq int
	err error
}

func (m Model) openDialog() Model {
	m.dialogs++
	d := &dialog{seq: m.dialogs, defaults: m.opts.Defaults, modelLists: m.opts.ModelChoices, efforts: effortsFor(domain.Harness(harnessChoices[0]))}
	start := m.opts.Defaults[domain.HarnessClaude]
	d.model, d.effort = start.Model, effortIndex(d.efforts, start.Effort)
	all := sorted(m.workspaces)
	last, found := domain.LastUsedWorkspace(all)
	d.spaces = all
	for i, w := range all {
		if found && w.Root == last.Root {
			d.ws, d.last = i, w.Root
		}
	}
	if dir := m.opts.LaunchDir; dir != "" {
		d.startIn(dir)
	}
	m.dialog = d
	return m
}

func sorted(ws map[string]domain.Workspace) []domain.Workspace {
	out := make([]domain.Workspace, 0, len(ws))
	for _, w := range ws {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

func effortIndex(choices []string, effort string) int {
	return max(slices.Index(choices, effort), 0)
}

// why: model and effort carry over only if the user left them at the previous
// harness's defaults.
func (d *dialog) cycleHarness(delta int) {
	prev := d.defaults[domain.Harness(harnessChoices[d.harness])]
	keep := ""
	if d.effort >= 0 && d.effort < len(d.efforts) {
		keep = d.efforts[d.effort]
	}
	d.harness = cycle(d.harness, delta, len(harnessChoices))
	next := d.defaults[domain.Harness(harnessChoices[d.harness])]
	restoredModel, restoredEffort := false, false
	if f := d.fallback; f != nil {
		d.fallback = nil
		if d.model == f.model {
			d.model, restoredModel = f.fromModel, true
		}
		if keep == f.effort {
			keep, restoredEffort = f.fromEffort, true
		}
	}
	if !restoredModel && d.model == prev.Model {
		d.model = next.Model
	}
	if !restoredEffort && keep == prev.Effort {
		keep = next.Effort
	}
	d.efforts = effortsFor(d.picked())
	if keep != "" && !slices.Contains(d.efforts, keep) {
		d.efforts = append(d.efforts, keep)
	}
	d.effort = effortIndex(d.efforts, keep)
	d.endTyping()
}

// why: an effort the picker does not list is added to it so it survives to
// submission.
func (d *dialog) takeFallback(req domain.StartRequest) {
	from := ""
	if d.effort >= 0 && d.effort < len(d.efforts) {
		from = d.efforts[d.effort]
	}
	trace := &fallbackTrace{fromModel: d.model, fromEffort: from}
	d.harness = slices.Index(harnessChoices, string(req.Harness))
	defaults := d.defaults[req.Harness]
	d.model = cmp.Or(req.Model, defaults.Model)
	d.efforts = effortsFor(req.Harness)
	effort := cmp.Or(req.Effort, defaults.Effort)
	if effort != "" && !slices.Contains(d.efforts, effort) {
		d.efforts = append(d.efforts, effort)
	}
	d.effort = effortIndex(d.efforts, effort)
	trace.model = d.model
	if d.effort >= 0 && d.effort < len(d.efforts) {
		trace.effort = d.efforts[d.effort]
	}
	d.fallback = trace
}

func cycle(i, delta, n int) int {
	if n == 0 {
		return 0
	}
	return ((i+delta)%n + n) % n
}

func (d *dialog) text() *string {
	switch {
	case d.field == fieldWorkItem:
		return &d.workItem
	case d.field == fieldModel && d.typing:
		return &d.model
	}
	return nil
}

func (d *dialog) picked() domain.Harness {
	return domain.Harness(harnessChoices[d.harness])
}

// why: omp has no fixed alias list, so the same model control takes typed text there only.
func (d *dialog) modelTypes() bool {
	return domain.Spec(d.picked()).Models == nil
}

func (d *dialog) modelChoices() []string {
	base := domain.SwitchChoices(d.picked(), domain.SwitchModel)
	if len(base) == 0 {
		base = d.modelLists[d.picked()]
	}
	out := append([]string{""}, base...)
	if d.model != "" && !slices.Contains(out, d.model) {
		out = append(out, d.model)
	}
	return out
}

const completionLimit = 6

func matchingModels(ids []string, query string) []string {
	if strings.TrimSpace(query) == "" || len(ids) == 0 {
		return nil
	}
	q := strings.ToLower(query)
	var prefix, rest []string
	for _, id := range ids {
		l := strings.ToLower(id)
		switch {
		case strings.HasPrefix(l, q):
			prefix = append(prefix, id)
		case strings.Contains(l, q):
			rest = append(rest, id)
		}
	}
	return append(prefix, rest...)
}

func completionWindow(all []string, i, limit int) ([]string, int) {
	if i < 0 || i >= len(all) {
		i = 0
	}
	if len(all) <= limit {
		return all, i
	}
	start := min(max(i-1, 0), len(all)-limit)
	return all[start : start+limit], i - start
}

func (d *dialog) menuMatches() []string {
	if !d.menu {
		return nil
	}
	return matchingModels(d.modelLists[d.picked()], d.query)
}

func (d *dialog) menuRows() ([]string, int) {
	all := d.menuMatches()
	if len(all) == 0 {
		return nil, 0
	}
	selected := d.suggest
	if selected < 0 || selected >= len(all) {
		selected = 0
	}
	return completionWindow(all, selected, completionLimit)
}

func (d *dialog) endTyping() {
	d.typing = false
	d.menu = false
	d.query = ""
	d.suggest = 0
}

func (d *dialog) cancelTyping() {
	if !d.typing {
		return
	}
	d.model = d.saved
	d.endTyping()
}

func (d *dialog) typeModel(s string) {
	if !d.typing {
		d.saved = d.model
		d.model = ""
		d.typing = true
	}
	if d.model != d.query {
		d.model = d.query
	}
	d.model += s
	d.query = d.model
	d.suggest = -1
	d.menu = true
}

func (d *dialog) modelBackspace() {
	if !d.typing {
		return
	}
	if d.model != d.query {
		d.model = d.query
	}
	r := []rune(d.query)
	if len(r) == 0 {
		d.cancelTyping()
		return
	}
	d.query = string(r[:len(r)-1])
	d.model = d.query
	d.suggest = 0
	if d.query == "" {
		d.cancelTyping()
		return
	}
	d.menu = true
}

func (d *dialog) moveMenu(delta int) bool {
	if !d.typing {
		return false
	}
	d.menu = true
	all := matchingModels(d.modelLists[d.picked()], d.query)
	if len(all) == 0 {
		d.menu = false
		return false
	}
	i := d.suggest
	if i < 0 {
		if delta > 0 {
			i = 0
		} else {
			i = len(all) - 1
		}
	} else {
		i = (i + delta + len(all)) % len(all)
	}
	d.suggest = i
	return true
}

func (d *dialog) acceptMenu() bool {
	if !d.menu {
		return false
	}
	shown, selected := d.menuRows()
	if len(shown) > 0 {
		d.model = shown[selected]
	}
	d.saved = d.model
	d.endTyping()
	return true
}

func (d *dialog) change(delta int) {
	switch d.field {
	case fieldWorkspace:
		d.ws = cycle(d.ws, delta, len(d.spaces))
	case fieldHarness:
		d.cycleHarness(delta)
	case fieldModel:
		d.cancelTyping()
		models := d.modelChoices()
		idx := slices.Index(models, d.model)
		if idx < 0 {
			idx = 0
		}
		d.model = models[cycle(idx, delta, len(models))]
		d.saved = d.model
	case fieldEffort:
		d.effort = cycle(d.effort, delta, len(d.efforts))
	}
}

// why: a copy, so an earlier Model value never changes with it.
func (m *Model) own() *dialog {
	d := *m.dialog
	m.dialog = &d
	return &d
}

func (m Model) dialogPaste(s string) Model {
	d := m.own()
	if d.busy {
		return m
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if d.field == fieldModel && d.modelTypes() {
		d.typeModel(s)
		return m
	}
	if t := d.text(); t != nil {
		*t += s
	}
	return m
}

func (m Model) dialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.own()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.opts.NewSessionOnly {
			return m, tea.Quit
		}
		// why: a start already sent keeps going; its reply still selects and shows the session.
		m.dialog = nil
		return m, nil
	}
	if d.busy || d.started {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+n":
		d.moveMenu(1)
	case "ctrl+p":
		d.moveMenu(-1)
	case "ctrl+y":
		d.acceptMenu()
	case "ctrl+e":
		d.cancelTyping()
	case "tab", "down":
		d.endTyping()
		d.field = field(cycle(int(d.field), 1, int(fieldCount)))
	case "shift+tab", "up":
		d.endTyping()
		d.field = field(cycle(int(d.field), -1, int(fieldCount)))
	case "left":
		d.change(-1)
	case "right":
		d.change(1)
	case "ctrl+s":
		if offer, ok := m.fallbackOffer(); ok {
			d.takeFallback(offer.Request)
		} else if advice, ok := m.advice(); ok && advice.OtherShortest != nil && advice.Other != domain.HarnessCodex {
			d.cycleHarness(slices.Index(harnessChoices, string(advice.Other)) - d.harness)
		}
	case "enter":
		return m, m.submit()
	case "backspace":
		if d.field == fieldModel && d.typing {
			d.modelBackspace()
			break
		}
		if t := d.text(); t != nil && *t != "" {
			r := []rune(*t)
			*t = string(r[:len(r)-1])
		}
	default:
		if msg.Text != "" {
			return m.dialogPaste(msg.Text), nil
		}
	}
	return m, nil
}

func (m Model) submit() tea.Cmd {
	d := m.dialog
	if d.menu {
		d.acceptMenu()
	}
	switch {
	case strings.TrimSpace(d.workItem) == "":
		d.err = "work item is empty"
		return nil
	case len(d.spaces) == 0:
		d.err = "no workspace: run agentws workspace add <path>"
		return nil
	}
	c := m.opts.Calls
	if c == nil {
		d.err = "not connected to the daemon"
		return nil
	}
	d.err, d.busy = "", true
	seq := d.seq
	p := rpc.NewSessionParams{
		Workspace: d.spaces[d.ws].Root,
		WorkItem:  strings.TrimSpace(d.workItem),
		Harness:   harnessChoices[d.harness],
		Model:     strings.TrimSpace(d.model),
		Effort:    d.efforts[d.effort],
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		var s domain.Session
		if err := c.Call(ctx, rpc.MethodNewSession, p, &s); err != nil {
			return startFailedMsg{seq, err}
		}
		return sessionStartedMsg{seq, s}
	}
}

func (m Model) call(method string, params any) tea.Cmd {
	c := m.opts.Calls
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := c.Call(ctx, method, params, nil); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) showNew(id string) tea.Cmd {
	return m.call(rpc.MethodSessionFocus, rpc.SessionFocusParams{ID: id})
}

// why: ends the popup's program only once the new session is in view, so
// closing the popup never races the focus call.
func (m Model) showNewAndQuit(id string) tea.Cmd {
	show := m.showNew(id)
	return func() tea.Msg {
		if show != nil {
			if e, ok := show().(errMsg); ok {
				return showFailedMsg(e)
			}
		}
		return tea.QuitMsg{}
	}
}

// why: keeps the popup open instead of closing on an error no one sees.
type showFailedMsg struct{ err error }

type popupFailedMsg struct{}

// why: if the daemon cannot run the popup, the dialog opens inline instead.
func (m Model) openPopup() tea.Cmd {
	c, p := m.opts.Calls, m.opts.DialogPopup
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := c.Call(ctx, rpc.MethodClientPopup, p, nil); err != nil {
			return popupFailedMsg{}
		}
		return nil
	}
}

func (m Model) endSession(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	return m.call(rpc.MethodEndSession, rpc.SessionRef{ID: id})
}

func (m Model) quotas() []domain.Quota {
	sessions := make([]domain.Session, 0, len(m.sessions))
	for _, x := range m.sessions {
		sessions = append(sessions, x)
	}
	return domain.Current(domain.Quotas(sessions), m.opts.Now())
}

func (m Model) chosenHarness() domain.Harness {
	return domain.Harness(harnessChoices[m.dialog.harness])
}

func (m Model) advice() (domain.SwitchAdvice, bool) {
	return domain.AdviseAt(m.quotas(), m.chosenHarness(), m.warnThreshold())
}

func (m Model) warnThreshold() int {
	if t := m.opts.Fallback.Threshold; t > 0 {
		return t
	}
	return domain.WarnQuotaLeft
}

func (m Model) fallbackOffer() (domain.FallbackOffer, bool) {
	d := m.dialog
	return domain.OfferFallback(m.quotas(), m.opts.Fallback, domain.StartRequest{
		Harness: m.chosenHarness(),
		Model:   strings.TrimSpace(d.model),
		Effort:  d.efforts[d.effort],
	})
}

func (d *dialog) startIn(dir string) {
	best := -1
	for i, w := range d.spaces {
		if (dir == w.Root || strings.HasPrefix(dir, w.Root+"/")) && (best < 0 || len(w.Root) > len(d.spaces[best].Root)) {
			best = i
		}
	}
	if best >= 0 {
		d.ws = best
		return
	}
	d.spaces = append([]domain.Workspace{{Root: dir}}, d.spaces...)
	d.ws = 0
}
