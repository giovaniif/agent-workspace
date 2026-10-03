package tui_test

import (
	"context"
	"maps"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type fakeOnboarder struct {
	mu           sync.Mutex
	state        domain.Onboarding
	installed    []domain.Harness
	finished     int
	nvimInstalls int
	err          error
}

func (f *fakeOnboarder) Onboarding(context.Context) (domain.Onboarding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeOnboarder) OnboardInstall(_ context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.HarnessSetup{}, f.err
	}
	f.installed = append(f.installed, h)
	s := f.state.Harnesses[h]
	s.Installed = true
	f.state.Harnesses = maps.Clone(f.state.Harnesses)
	f.state.Harnesses[h] = s
	return s, nil
}

func (f *fakeOnboarder) OnboardFinish(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished++
	f.state.Done = true
	return nil
}

func (f *fakeOnboarder) OnboardNvim(context.Context) (domain.NvimSetup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.NvimSetup{}, f.err
	}
	f.nvimInstalls++
	f.state.Nvim.Configured = true
	return f.state.Nvim, nil
}
