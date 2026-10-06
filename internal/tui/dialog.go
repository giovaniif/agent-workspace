package tui

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

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
	field           field
	workItem        string
	model           string
	spaces          []domain.Workspace
	last            string
	ws              int
	harness         int
	effort          int
	efforts         []string
	fallback        *fallbackTrace
	err             string
	busy            bool
	started         bool
	seq             int
	defaults        map[domain.Harness]Defaults
	modelLists      map[domain.Harness][]string
	typing          bool
	menu            bool
	query           string
	saved           string
	suggest         int
	menuAt, menuCol int
	path            string
	base, home      string
	listing         listing
	listings        int
	pick            int
}

type listing struct {
	req    int
	dir    string
	dirs   []domain.Child
	done   bool
	failed bool
}

type dirsListedMsg struct {
	seq    int
	req    int
	dir    string
	dirs   []domain.Child
	failed bool
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
	d.spaces = all
	if last, found := domain.LastCreatedSession(sessionsOf(m.sessions)); found {
		d.seedCreated(last)
		if d.last == "" {
			if used, found := domain.LastUsedWorkspace(all); found {
				d.selectWorkspace(used.Root)
			}
		}
	} else if dir := m.opts.LaunchDir; dir != "" {
		d.startIn(dir)
	} else if used, found := domain.LastUsedWorkspace(all); found {
		d.selectWorkspace(used.Root)
	}
	d.home = m.opts.Home
	d.base = cmp.Or(m.opts.LaunchDir, d.root(), d.home, "/")
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

func (d *dialog) selectWorkspace(root string) {
	for i, w := range d.spaces {
		if w.Root == root {
			d.ws, d.last = i, w.Root
		}
	}
}

func sessionsOf(all map[string]domain.Session) []domain.Session {
	out := make([]domain.Session, 0, len(all))
	for _, s := range all {
		out = append(out, s)
	}
	return out
}

func (d *dialog) seedCreated(s domain.Session) {
	if i := slices.Index(harnessChoices, string(s.Harness)); i >= 0 {
		d.harness = i
		d.efforts = effortsFor(s.Harness)
	}
	d.model = s.StartModel
	d.selectWorkspace(s.StartWorkspace)
	if !slices.Contains(d.efforts, s.StartEffort) {
		d.efforts = append(d.efforts, s.StartEffort)
	}
	d.effort = effortIndex(d.efforts, s.StartEffort)
}

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
	switch d.field {
	case fieldWorkItem:
		return &d.workItem
	case fieldWorkspace:
		return &d.path
	case fieldModel:
		if d.typing {
			return &d.model
		}
	}
	return nil
}

func (d *dialog) picked() domain.Harness {
	return domain.Harness(harnessChoices[d.harness])
}

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

func (d *dialog) root() string {
	if len(d.spaces) == 0 {
		return ""
	}
	return d.spaces[d.ws].Root
}

func (d *dialog) typed() domain.PathInput {
	return domain.ParsePathInput(d.path, d.base, d.home)
}

func (d *dialog) chosen() (domain.Workspace, bool) {
	if d.path == "" {
		if len(d.spaces) == 0 {
			return domain.Workspace{}, false
		}
		return d.spaces[d.ws], true
	}
	root := d.typed().Path
	for _, w := range d.spaces {
		if w.Root == root && w.Kind != "" {
			return w, true
		}
	}
	return domain.Workspace{Root: root}, true
}

func (d *dialog) matches() []domain.Child {
	in := d.typed()
	if d.path == "" || !d.listing.done || d.listing.dir != in.Dir {
		return nil
	}
	return domain.CompleteDirs(d.listing.dirs, in.Prefix)
}

type folderState int

const (
	folderPending folderState = iota
	folderFound
	folderMissing
)

func (d *dialog) folder() folderState {
	in := d.typed()
	if !d.listing.done || d.listing.dir != in.Dir {
		return folderPending
	}
	if d.listing.failed {
		return folderMissing
	}
	if in.Prefix == "" || in.Prefix == "." || in.Prefix == ".." {
		return folderFound
	}
	for _, c := range d.listing.dirs {
		if c.Name == in.Prefix {
			return folderFound
		}
	}
	return folderMissing
}

func (d *dialog) open() bool {
	ms := d.matches()
	if len(ms) == 0 {
		return false
	}
	head := d.path[:strings.LastIndex(d.path, "/")+1]
	if d.path == "~" {
		head = "~/"
	}
	d.path = head + ms[min(d.pick, len(ms)-1)].Name + "/"
	return true
}

func (d *dialog) up() {
	t := strings.TrimSuffix(d.path, "/")
	cut := strings.LastIndex(t, "/")
	seg := t[cut+1:]
	switch {
	case t == "":
		d.path = "/"
	case t == ".":
		d.path = "../"
	case seg == "..":
		d.path = t + "/../"
	case t == "~":
		d.path = filepath.Dir(d.home) + "/"
	case cut < 0:
		d.path = "./"
	default:
		d.path = t[:cut+1]
	}
}

func (m Model) listDirs() tea.Cmd {
	d := m.dialog
	d.pick = 0
	c := m.opts.Calls
	if d.path == "" || c == nil {
		return nil
	}
	dir := d.typed().Dir
	if d.listing.dir == dir && !d.listing.failed {
		return nil
	}
	d.listings++
	d.listing = listing{req: d.listings, dir: dir}
	seq, req := d.seq, d.listings
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var out rpc.WorkspaceDirs
		err := c.Call(ctx, rpc.MethodWorkspaceDirs, rpc.WorkspaceDirsParams{Path: dir}, &out)
		return dirsListedMsg{seq: seq, req: req, dir: dir, dirs: out.Dirs, failed: err != nil}
	}
}

func (m Model) gotDirs(msg dirsListedMsg) Model {
	if m.dialog == nil || m.dialog.seq != msg.seq || m.dialog.listing.req != msg.req {
		return m
	}
	m.own().listing = listing{req: msg.req, dir: msg.dir, dirs: msg.dirs, done: true, failed: msg.failed}
	return m
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

func (m *Model) own() *dialog {
	d := *m.dialog
	m.dialog = &d
	return &d
}

func (m Model) dialogPaste(s string) (Model, tea.Cmd) {
	d := m.own()
	if d.busy {
		return m, nil
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if d.field == fieldModel && d.modelTypes() {
		d.typeModel(s)
		return m, nil
	}
	if t := d.text(); t != nil {
		*t += s
	}
	if m.dialog.field == fieldWorkspace {
		return m, m.listDirs()
	}
	return m, nil
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
		m.dialog = nil
		return m, nil
	}
	if d.busy || d.started {
		return m, nil
	}
	if d.field == fieldWorkspace && d.path != "" {
		if next, cmd, ok := m.pathKey(msg.String()); ok {
			return next, cmd
		}
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
		if d.field == fieldModel && d.model != "" {
			d.model, d.saved = "", ""
			break
		}
		if d.field == fieldEffort && d.effort > 0 {
			d.effort = 0
			break
		}
		if t := d.text(); t != nil && *t != "" {
			r := []rune(*t)
			*t = string(r[:len(r)-1])
			return m, m.listDirs()
		}
	default:
		if msg.Text != "" {
			return m.dialogPaste(msg.Text)
		}
	}
	return m, nil
}

func (m Model) pathKey(key string) (tea.Model, tea.Cmd, bool) {
	d := m.dialog
	n := len(d.matches())
	switch {
	case key == "down" && n > 0:
		d.pick = min(d.pick+1, n-1)
	case key == "up" && n > 0:
		d.pick = max(d.pick-1, 0)
	case key == "right":
		if d.open() {
			return m, m.listDirs(), true
		}
	case key == "left":
		d.up()
		return m, m.listDirs(), true
	default:
		return m, nil, false
	}
	return m, nil, true
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
	}
	space, ok := d.chosen()
	if !ok {
		d.err = "no workspace: type a folder's path"
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
		Workspace: space.Root,
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

type showFailedMsg struct{ err error }

type popupFailedMsg struct{}

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
