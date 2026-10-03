package daemon_test

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type fakeOnboarder struct {
	mu           sync.Mutex
	state        domain.Onboarding
	installed    []domain.Harness
	failOn       domain.Harness
	nvimInstalls int
}

func (f *fakeOnboarder) Onboarding(context.Context) (domain.Onboarding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeOnboarder) Install(_ context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h == f.failOn {
		return domain.HarnessSetup{}, errors.New("settings.json is not valid JSON")
	}
	f.installed = append(f.installed, h)
	setup := domain.HarnessSetup{Installed: true, File: "/c/" + string(h)}
	f.state.Harnesses = maps.Clone(f.state.Harnesses)
	if f.state.Harnesses == nil {
		f.state.Harnesses = map[domain.Harness]domain.HarnessSetup{}
	}
	f.state.Harnesses[h] = setup
	return setup, nil
}

func (f *fakeOnboarder) Finish(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.Done = true
	return nil
}

func (f *fakeOnboarder) InstallNvim(context.Context) (domain.NvimSetup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.Nvim.Configured = true
	f.nvimInstalls++
	return f.state.Nvim, nil
}
