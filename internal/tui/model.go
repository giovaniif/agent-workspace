package tui

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type Focuser interface {
	FocusMain(ctx context.Context) error
}

type Attender interface {
	MuteSession(ctx context.Context, id string, muted bool) error
	FocusSession(ctx context.Context, id string) error
}

type Killer interface {
	KillPorts(ctx context.Context, pgids []int) ([]int, error)
}

type Options struct {
	Theme           Theme
	Now             func() time.Time
	Tick            time.Duration
	Focus           Focuser
	Attend          Attender
	Kill            Killer
	Defaults        map[domain.Harness]Defaults
	Fallback        domain.FallbackConfig
	Calls           Caller
	Switch          Switcher
	Review          Reviewer
	Disk            Disker
	DialogPopup     rpc.ClientPopupParams
	HarnessDefaults map[domain.Harness]Defaults
	ModelChoices    map[domain.Harness][]string
	RefreshModels   tea.Cmd
	NewSessionOnly  bool
	LaunchDir       string
	Home            string
	Onboard         Onboarder
	SetupOnly       bool
	SetupPopup      rpc.ClientPopupParams
	NoMouse         bool
	Redial          func(context.Context) (Connection, error)
	CanRestart      bool
	Project         string
}

type Caller interface {
	Call(ctx context.Context, method string, params, out any) error
}

type StateMsg rpc.State

type DiffMsg rpc.Diff

type TickMsg struct{}

type ModelsMsg struct {
	Choices map[domain.Harness][]string
}

type TopBarMsg struct {
	Claude string
	Codex  string
	Disk   string
}

type DisconnectedMsg struct{ Err error }

type errMsg struct{ err error }

type entry struct {
	session    domain.Session
	task       domain.Task
	num        int
	groupStart bool
	groupRepos []string
	worktrees  []domain.Worktree
}

type Model struct {
	opts   Options
	styles styles
	width  int
	height int

	workspaces map[string]domain.Workspace
	projects   map[string]domain.Project
	proj       *projectsPanel
	shellTabs  map[string]domain.ShellTab
	activeTabs map[string]string
	tabNew     *tabChooser
	closingTab *tabClose
	tasks      map[string]domain.Task
	taskOrder  []string
	worktrees  map[string]domain.Worktree
	sessions   map[string]domain.Session
	events     map[string][]domain.SessionEvent
	subagents  map[string][]domain.Subagent
	entries    []entry
	collapsed  map[string]bool
	scroll     int
	scrolled   bool

	selected string
	last     string
	help     bool
	picker   *picker
	resuming *resumePicker
	frame    int
	top      TopBarMsg
	status   string
	confirm  *killPrompt
	menu     *sessionMenu
	rv       reviewState
	dk       diskState
	paint    *painter
	renaming *renamePrompt
	drafts   map[string]domain.ReviewDraft

	queue     []domain.LaunchItem
	launching *launchInput
	launches  int

	dialog  *dialog
	dialogs int
	ending  string
	pending string

	ob             *onboarding
	onboardChecked bool

	disconnected bool
	rc           reconnect
	restart      bool
	startup      tea.Cmd
}

func New(opts Options) Model {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return Model{
		opts:       opts,
		styles:     newStyles(opts.Theme),
		width:      40,
		height:     24,
		workspaces: map[string]domain.Workspace{},
		projects:   map[string]domain.Project{},
		drafts:     map[string]domain.ReviewDraft{},
		tasks:      map[string]domain.Task{},
		worktrees:  map[string]domain.Worktree{},
		sessions:   map[string]domain.Session{},
		events:     map[string][]domain.SessionEvent{},
		subagents:  map[string][]domain.Subagent{},
		collapsed:  map[string]bool{},
		paint:      newPainter(opts.Theme),
	}
}

func (m Model) Selected() string { return m.selected }

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.tick(), m.opts.RefreshModels, m.startup)
}

func (m Model) applyModels(choices map[domain.Harness][]string) Model {
	m.opts.ModelChoices = choices
	if m.dialog != nil {
		m.dialog.modelLists = choices
	}
	p := m.picker
	if p != nil && p.kind == domain.SwitchModel && domain.Spec(p.harness).Models == nil {
		p.choices = nil
		if choices != nil {
			p.choices = choices[p.harness]
		}
		p.typed = len(p.choices) == 0
		p.cursor = 0
	}
	return m
}

func (m Model) tick() tea.Cmd {
	if m.opts.Tick <= 0 {
		return nil
	}
	return tea.Tick(m.opts.Tick, func(time.Time) tea.Msg { return TickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case ModelsMsg:
		m = m.applyModels(msg.Choices)
	case StateMsg:
		m.load(rpc.State(msg))
		if m.opts.NewSessionOnly && m.dialog == nil {
			return m.openDialog(), nil
		}
		if m.opts.Onboard != nil && !m.onboardChecked && !m.opts.NewSessionOnly {
			m.onboardChecked = true
			return m, m.fetchOnboarding(false)
		}
	case onboardStatusMsg, onboardInstalledMsg, onboardFinishedMsg, onboardNvimMsg, setupPopupFailedMsg, setupPopupRetryMsg:
		return m.onboardMsg(msg)
	case popupFailedMsg:
		return m.openDialogIn(msg.project), nil
	case projectAddedMsg:
		m = m.projectAddDone(msg.seq, nil)
	case projectAddFailedMsg:
		m = m.projectAddDone(msg.seq, msg.err)
	case showFailedMsg:
		if m.dialog != nil {
			d := m.own()
			d.busy, d.started = false, true
			d.err = "session started, but showing it failed: " + msg.err.Error() + " · esc closes"
		}
	case DiffMsg:
		m.apply(rpc.Diff(msg))
	case TickMsg:
		m.frame++
		return m, tea.Batch(m.tick(), m.tickDisk())
	case diskMsg:
		m.gotDisk(msg)
	case diskDoneMsg:
		return m, m.gotDiskAction(msg)
	case diskShellMsg:
		if msg.err != nil {
			m.dk.status = msg.err.Error()
			break
		}
		return m.closeDisk()
	case TopBarMsg:
		m.top = msg
	case DisconnectedMsg:
		return m.disconnect()
	case RedialMsg:
		return m.redial()
	case redialedMsg:
		return m.redialed(msg)
	case streamDiffMsg:
		m.apply(msg.diff)
		return m, listen(msg.diffs)
	case errMsg:
		m.status = msg.err.Error()
	case reviewMsg:
		m.gotReview(msg)
	case draftMsg:
		return m.gotDraft(msg)
	case tea.KeyPressMsg:
		if m.disconnected && m.quitKey(msg.String()) {
			return m, tea.Quit
		}
		if m.rc.upgraded && m.opts.CanRestart && msg.String() == "r" && !m.typing() {
			m.restart = true
			return m, tea.Quit
		}
		if m.ob != nil {
			return m.onboardKey(msg)
		}
		if m.dialog != nil {
			return m.dialogKey(msg)
		}
		if m.rv.open {
			return m.reviewKey(msg)
		}
		if m.proj != nil {
			return m.projectsKey(msg)
		}
		if m.launching != nil {
			return m.launcherKey(msg)
		}
		if m.renaming != nil {
			return m.renameKey(msg)
		}
		if m.dk.open {
			return m.diskKey(msg.String())
		}
		if m.tabNew != nil {
			return m.tabChooserKey(msg)
		}
		if m.menu != nil {
			return m.menuKey(msg)
		}
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.PasteMsg:
		if m.dialog != nil {
			return m.dialogPaste(msg.Content)
		}
		if m.proj != nil {
			return m.projectPaste(msg.Content), nil
		}
		if m.launching != nil {
			return m.launcherPaste(msg.Content), nil
		}
		if m.renaming != nil {
			return m.renamePaste(msg.Content), nil
		}
	case sessionStartedMsg:
		if m.opts.NewSessionOnly {
			return m, m.showNewAndQuit(msg.session.ID)
		}
		if m.dialog != nil && m.dialog.seq == msg.seq {
			m.dialog = nil
		}
		m.pending = msg.session.ID
		m.choosePending()
		return m, m.showNew(msg.session.ID)
	case launchSentMsg:
		if m.launching != nil && m.launching.seq == msg.seq {
			m.launching = nil
		}
	case launchFailedMsg:
		if m.launching != nil && m.launching.seq == msg.seq {
			in := *m.launching
			in.busy, in.err = false, msg.err.Error()
			m.launching = &in
		}
	case dirsListedMsg:
		return m.gotDirs(msg), nil
	case startFailedMsg:
		if m.dialog == nil || m.dialog.seq != msg.seq {
			m.status = msg.err.Error()
			break
		}
		d := m.own()
		d.busy, d.err = false, msg.err.Error()
	}
	return m, nil
}

func (m *Model) load(st rpc.State) {
	m.workspaces = map[string]domain.Workspace{}
	for _, w := range st.Workspaces {
		m.workspaces[w.Root] = w
	}
	m.projects = map[string]domain.Project{}
	for _, p := range st.Projects {
		m.projects[p.Root] = p
	}
	m.shellTabs = map[string]domain.ShellTab{}
	for _, sh := range st.ShellTabs {
		m.shellTabs[sh.ID] = sh
	}
	m.activeTabs = maps.Clone(st.ActiveTabs)
	if m.activeTabs == nil {
		m.activeTabs = map[string]string{}
	}
	m.tasks = map[string]domain.Task{}
	m.worktrees = map[string]domain.Worktree{}
	m.sessions = map[string]domain.Session{}
	m.events = map[string][]domain.SessionEvent{}
	m.subagents = map[string][]domain.Subagent{}
	m.taskOrder = nil
	m.queue = st.Queue
	m.drafts = map[string]domain.ReviewDraft{}
	for _, d := range st.Drafts {
		m.putDraft(d)
	}
	for _, t := range st.Tasks {
		m.putTask(t)
	}
	for _, w := range st.Worktrees {
		m.worktrees[w.ID] = w
	}
	for _, s := range st.Sessions {
		m.sessions[s.ID] = s
	}
	for _, ev := range st.Events {
		m.addEvent(ev)
	}
	for _, sub := range st.Subagents {
		m.putSubagent(sub)
	}
	m.rebuild()
}

func (m *Model) addEvent(ev domain.SessionEvent) {
	kept := append(m.events[ev.SessionID], ev)
	if len(kept) > domain.SessionEventsKept {
		kept = kept[len(kept)-domain.SessionEventsKept:]
	}
	m.events[ev.SessionID] = kept
}

func (m *Model) apply(d rpc.Diff) {
	if d.Event != nil {
		m.addEvent(*d.Event)
	}
	switch {
	case d.Draft != nil:
		m.putDraft(*d.Draft)
		return
	case d.Workspace != nil:
		m.workspaces[d.Workspace.Root] = *d.Workspace
		return
	case d.RemovedWorkspace != "":
		delete(m.workspaces, d.RemovedWorkspace)
		return
	case d.Project != nil:
		m.projects[d.Project.Root] = *d.Project
		return
	case d.RemovedProject != "":
		delete(m.projects, d.RemovedProject)
		return
	case d.ShellTab != nil:
		m.shellTabs[d.ShellTab.ID] = *d.ShellTab
		return
	case d.RemovedShellTab != "":
		delete(m.shellTabs, d.RemovedShellTab)
		return
	case d.ActiveTab != nil:
		m.activeTabs[d.ActiveTab.Worktree] = d.ActiveTab.Tab
		return
	case d.Task != nil:
		m.putTask(*d.Task)
	case d.RemovedWorktree != "":
		delete(m.worktrees, d.RemovedWorktree)
	case d.Worktree != nil:
		m.worktrees[d.Worktree.ID] = *d.Worktree
	case d.RemovedSession != "":
		delete(m.sessions, d.RemovedSession)
		delete(m.events, d.RemovedSession)
		delete(m.subagents, d.RemovedSession)
	case d.Session != nil:
		if d.Session.Focused && !m.sessions[d.Session.ID].Focused {
			m.pending = d.Session.ID
		}
		m.sessions[d.Session.ID] = *d.Session
	case d.Subagent != nil:
		m.putSubagent(*d.Subagent)
		return
	case d.Queue != nil:
		m.queue = *d.Queue
		return
	default:
		return
	}
	m.rebuild()
}

func (m *Model) putTask(t domain.Task) {
	if _, ok := m.tasks[t.ID]; !ok {
		m.taskOrder = append(m.taskOrder, t.ID)
	}
	m.tasks[t.ID] = t
}

func (m *Model) rebuild() {
	was := m.index(m.selected)
	tasks := make([]domain.Task, 0, len(m.taskOrder))
	for _, id := range m.taskOrder {
		tasks = append(tasks, m.tasks[id])
	}
	sessions := make([]domain.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })

	m.entries = make([]entry, 0, len(sessions))
	for _, g := range domain.Sidebar(tasks, sessions) {
		var repos []string
		seen := map[string]bool{}
		start := len(m.entries)
		for _, s := range g.Sessions {
			e := entry{session: s, task: g.Task, num: len(m.entries) + 1}
			for _, id := range s.WorktreeIDs {
				w, ok := m.worktrees[id]
				if !ok {
					continue
				}
				e.worktrees = append(e.worktrees, w)
				if name := repoName(w); !seen[w.Repo] {
					seen[w.Repo] = true
					repos = append(repos, name)
				}
			}
			m.entries = append(m.entries, e)
		}
		m.entries[start].groupStart = true
		m.entries[start].groupRepos = repos
	}
	if m.index(m.selected) < 0 {
		m.selected = ""
		if len(m.entries) > 0 {
			m.selected = m.entries[min(max(was, 0), len(m.entries)-1)].session.ID
		}
	}
	if m.index(m.last) < 0 {
		m.last = ""
	}
	m.choosePending()
	m.closeStaleMenu()
}

func (m *Model) choosePending() {
	if i := m.index(m.pending); i >= 0 {
		m.choose(i)
		m.pending = ""
	}
}

func (m Model) index(id string) int {
	if id == "" {
		return -1
	}
	for i, e := range m.entries {
		if e.session.ID == id {
			return i
		}
	}
	return -1
}

func (m *Model) choose(i int) {
	if i < 0 || i >= len(m.entries) {
		return
	}
	id := m.entries[i].session.ID
	if id == m.selected {
		return
	}
	m.last, m.selected = m.selected, id
	m.scrolled = false
	m.closeStaleMenu()
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if prompt := m.confirm; prompt != nil {
		m.confirm = nil
		if k == "y" {
			return m, m.kill(prompt.pgids)
		}
		return m, nil
	}
	if m.picker != nil {
		return m.pickerKey(k)
	}
	if m.resuming != nil {
		return m.resumeKey(k)
	}
	cur := m.index(m.selected)
	if m.closingTab != nil {
		return m.confirmCloseTab(k)
	}
	if m.ending != "" {
		id := m.ending
		m.ending, m.status = "", ""
		if k == "y" {
			return m, m.endSession(id)
		}
		return m, nil
	}
	if next, cmd, handled := m.tabKey(k); handled {
		return next, cmd
	}
	switch k {
	case "n":
		switch {
		case m.opts.Calls == nil:
		case len(m.opts.DialogPopup.Command) > 0:
			return m, m.openPopup()
		default:
			return m.openDialog(), nil
		}
	case "S":
		if m.opts.Onboard != nil {
			return m, m.openSetup(setupPopupRetries)
		}
	case "L":
		if m.opts.Calls != nil {
			return m.openLauncher(), nil
		}
	case "P":
		if m.opts.Calls != nil {
			return m.openProjects(), nil
		}
	case "c":
		return m, m.takeQueueOffers()
	case "X":
		return m, m.clearQueue()
	case "x":
		if cur >= 0 && m.opts.Calls != nil {
			m.ending = m.selected
			m.status = fmt.Sprintf("end session %d? y/n", m.entries[cur].num)
		}
	case "q", "ctrl+c":
		return m, m.leave()
	case "?":
		m.help = !m.help
	case "u":
		m = m.openResume()
	case "M":
		m = m.openPicker(domain.SwitchModel)
	case "E":
		m = m.openPicker(domain.SwitchEffort)
	case "j", "down":
		m.choose(cur + 1)
	case "k", "up":
		m.choose(cur - 1)
	case "tab":
		m.choose(m.index(m.last))
	case "space":
		m.choose(m.nextNeedingYou(cur))
	case "o":
		if m.selected != "" {
			m.collapsed[m.selected] = !m.collapsed[m.selected]
		}
	case "R":
		return m.askRename(), nil
	case "A":
		return m, m.unpin()
	case "m":
		return m, m.toggleMute()
	case "K":
		m.askKill()
	case "enter":
		return m, m.focus()
	case "r":
		return m.openReview()
	case "w":
		return m.openDisk()
	case "t":
		return m, m.toggleShell(false)
	case "T":
		return m, m.toggleShell(true)
	case "s":
		return m, m.focusShell()
	case "e":
		return m, m.toggleNvim()
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			m.choose(int(k[0] - '1'))
		}
	}
	return m, nil
}

func (m Model) nextNeedingYou(cur int) int {
	n := len(m.entries)
	for step := 1; step <= n; step++ {
		i := (cur + step + n) % n
		if m.entries[i].session.NeedsYou() {
			return i
		}
	}
	return -1
}

func (m Model) focus() tea.Cmd {
	f := m.opts.Focus
	if f == nil || m.selected == "" {
		return nil
	}
	id, attend := m.selected, m.opts.Attend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := f.FocusMain(ctx); err != nil {
			return errMsg{err}
		}
		if attend != nil {
			if err := attend.FocusSession(ctx, id); err != nil {
				return errMsg{err}
			}
		}
		return nil
	}
}

func (m Model) toggleMute() tea.Cmd {
	a := m.opts.Attend
	x, ok := m.sessions[m.selected]
	if a == nil || !ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := a.MuteSession(ctx, x.ID, !x.Muted); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *Model) askKill() {
	i := m.index(m.selected)
	if i < 0 || m.opts.Kill == nil {
		return
	}
	ports := m.entries[i].ports()
	if len(ports) == 0 {
		return
	}
	m.confirm = &killPrompt{pgids: groupsOf(ports), label: portLabel(ports)}
}

func (m Model) kill(pgids []int) tea.Cmd {
	k := m.opts.Kill
	if k == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), killTimeout)
		defer cancel()
		if _, err := k.KillPorts(ctx, pgids); err != nil {
			return errMsg{err}
		}
		return nil
	}
}
