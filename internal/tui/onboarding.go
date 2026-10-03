package tui

import (
	"context"
	"maps"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type Onboarder interface {
	Onboarding(ctx context.Context) (domain.Onboarding, error)
	OnboardInstall(ctx context.Context, h domain.Harness) (domain.HarnessSetup, error)
	OnboardNvim(ctx context.Context) (domain.NvimSetup, error)
	OnboardFinish(ctx context.Context) error
}

// why: on the very first run the sidebar starts before tmux has attached a client, so the popup can fail at first.
const (
	setupPopupRetries = 2
	setupPopupRetry   = 400 * time.Millisecond
)

type onboardResult int

const (
	resultNone onboardResult = iota
	resultInstalled
	resultSkipped
)

type onboarding struct {
	status  domain.Onboarding
	step    domain.OnboardStep
	picked  []domain.Harness
	cursor  int
	busy    bool
	err     string
	results map[domain.OnboardStep]onboardResult
}

func newOnboarding(o domain.Onboarding) *onboarding {
	return &onboarding{status: o, step: domain.OnboardPick, picked: domain.DefaultOnboardPicks(o), results: map[domain.OnboardStep]onboardResult{}}
}

func (ob *onboarding) steps() []domain.OnboardStep { return domain.OnboardSteps(ob.picked) }

func (ob *onboarding) harness() (domain.Harness, domain.HarnessSetup) {
	h, _ := ob.step.Harness()
	return h, ob.status.Harnesses[h]
}

func (ob *onboarding) togglePick(h domain.Harness) {
	if i := slices.Index(ob.picked, h); i >= 0 {
		ob.picked = slices.Delete(slices.Clone(ob.picked), i, i+1)
		return
	}
	ob.picked = append(slices.Clone(ob.picked), h)
}

func (ob *onboarding) advance(r onboardResult) {
	if r != resultNone {
		ob.results[ob.step] = r
	}
	ob.err = ""
	ob.step = domain.NextOnboardStep(ob.steps(), ob.step)
}

type onboardStatusMsg struct {
	o    domain.Onboarding
	err  error
	open bool
}

type onboardInstalledMsg struct {
	h     domain.Harness
	setup domain.HarnessSetup
	err   error
}

type onboardFinishedMsg struct{ err error }

type onboardNvimMsg struct {
	n   domain.NvimSetup
	err error
}

type setupPopupFailedMsg struct{ tries int }

type setupPopupRetryMsg struct{ tries int }

// why: open says to show the walkthrough even when it was done, as S and a failed popup ask.
func (m Model) fetchOnboarding(open bool) tea.Cmd {
	o := m.opts.Onboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		st, err := o.Onboarding(ctx)
		return onboardStatusMsg{o: st, err: err, open: open}
	}
}

func (m Model) openSetup(tries int) tea.Cmd {
	c, p := m.opts.Calls, m.opts.SetupPopup
	if c == nil || len(p.Command) == 0 {
		return m.fetchOnboarding(true)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := c.Call(ctx, rpc.MethodClientPopup, p, nil); err != nil {
			return setupPopupFailedMsg{tries: tries}
		}
		return nil
	}
}

func (m Model) onboardMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case onboardStatusMsg:
		switch {
		case msg.err != nil:
			m.status = "setup: " + msg.err.Error()
			if m.opts.SetupOnly {
				return m, tea.Quit
			}
		case msg.open || m.opts.SetupOnly:
			m.ob = newOnboarding(msg.o)
		case domain.OnboardingNeeded(msg.o):
			return m, m.openSetup(0)
		case !msg.o.Done:
			// why: nothing is left to set up, so the marker is written without showing anything.
			return m, m.finishOnboarding()
		}
	case setupPopupFailedMsg:
		if msg.tries < setupPopupRetries {
			next := msg.tries + 1
			return m, tea.Tick(setupPopupRetry, func(time.Time) tea.Msg { return setupPopupRetryMsg{tries: next} })
		}
		return m, m.fetchOnboarding(true)
	case setupPopupRetryMsg:
		return m, m.openSetup(msg.tries)
	case onboardNvimMsg:
		if m.ob == nil {
			return m, nil
		}
		ob := *m.ob
		ob.busy = false
		if msg.err != nil {
			ob.err = msg.err.Error()
		} else {
			ob.err = ""
			ob.status.Nvim = msg.n
			ob.results = copyResults(ob.results)
			ob.results[domain.OnboardNvim] = resultInstalled
		}
		m.ob = &ob
	case onboardInstalledMsg:
		if m.ob == nil {
			return m, nil
		}
		ob := *m.ob
		ob.busy = false
		if msg.err != nil {
			ob.err = msg.err.Error()
		} else {
			ob.err = ""
			ob.results = copyResults(ob.results)
			ob.results[ob.step] = resultInstalled
			ob.status.Harnesses = maps.Clone(ob.status.Harnesses)
			if ob.status.Harnesses == nil {
				ob.status.Harnesses = map[domain.Harness]domain.HarnessSetup{}
			}
			ob.status.Harnesses[msg.h] = msg.setup
		}
		m.ob = &ob
	case onboardFinishedMsg:
		if msg.err != nil && m.ob != nil {
			ob := *m.ob
			ob.busy, ob.err = false, msg.err.Error()
			m.ob = &ob
			return m, nil
		}
		m.ob = nil
		if m.opts.SetupOnly {
			return m, tea.Quit
		}
	}
	return m, nil
}

func copyResults(r map[domain.OnboardStep]onboardResult) map[domain.OnboardStep]onboardResult {
	out := make(map[domain.OnboardStep]onboardResult, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out
}

func (m Model) onboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ob := *m.ob
	ob.results = copyResults(ob.results)
	m.ob = &ob
	k := msg.String()
	if k == "ctrl+c" {
		if m.opts.SetupOnly {
			return m, tea.Quit
		}
		m.ob = nil
		return m, nil
	}
	if ob.busy {
		return m, nil
	}
	if k == "esc" {
		ob.busy = true
		return m, m.finishOnboarding()
	}
	_, harnessStep := ob.step.Harness()
	switch {
	case ob.step == domain.OnboardPick:
		harnesses := domain.Harnesses()
		switch k {
		case "up", "k", "shift+tab":
			ob.cursor = max(ob.cursor-1, 0)
		case "down", "j", "tab":
			ob.cursor = min(ob.cursor+1, len(harnesses)-1)
		case "space", "x":
			ob.togglePick(harnesses[ob.cursor])
		case "enter":
			ob.advance(resultNone)
		}
	case harnessStep:
		h, setup := ob.harness()
		switch k {
		case "s":
			if ob.results[ob.step] == resultInstalled {
				ob.advance(resultNone)
			} else {
				ob.advance(resultSkipped)
			}
		case "enter":
			if domain.HarnessOffer(setup) == domain.OfferInstall && ob.results[ob.step] != resultInstalled {
				ob.busy = true
				return m, m.install(h)
			}
			ob.advance(resultNone)
		}
	case ob.step == domain.OnboardNvim:
		switch k {
		case "s":
			if ob.results[ob.step] == resultInstalled {
				ob.advance(resultNone)
			} else {
				ob.advance(resultSkipped)
			}
		case "enter":
			if domain.NvimOfferFor(ob.status.Nvim) == domain.NvimShowSnippet && ob.results[ob.step] != resultInstalled {
				ob.busy = true
				return m, m.installNvim()
			}
			ob.advance(resultNone)
		}
	case ob.step == domain.OnboardFinish:
		if k == "enter" {
			ob.busy = true
			return m, m.finishOnboarding()
		}
	}
	return m, nil
}

func (m Model) install(h domain.Harness) tea.Cmd {
	o := m.opts.Onboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := o.OnboardInstall(ctx, h)
		return onboardInstalledMsg{h: h, setup: s, err: err}
	}
}

func (m Model) finishOnboarding() tea.Cmd {
	o := m.opts.Onboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return onboardFinishedMsg{err: o.OnboardFinish(ctx)}
	}
}

func (m Model) installNvim() tea.Cmd {
	o := m.opts.Onboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		n, err := o.OnboardNvim(ctx)
		return onboardNvimMsg{n: n, err: err}
	}
}
