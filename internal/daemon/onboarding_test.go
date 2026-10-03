package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestOnboardingReportsInstallsAndFinishes(t *testing.T) {
	f := &fakeOnboarder{state: domain.Onboarding{Nvim: domain.NvimSetup{OnPath: true}}}
	_, path := start(t, &memStore{}, daemon.WithOnboarding(f))
	c := dial(t, path)
	ctx := context.Background()

	got, err := c.Onboarding(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done || !got.Nvim.OnPath {
		t.Errorf("status = %+v", got)
	}

	setup, err := c.OnboardInstall(ctx, domain.HarnessCodex)
	if err != nil || !setup.Installed || setup.File != "/c/codex" {
		t.Errorf("install = %+v, %v", setup, err)
	}
	if err := c.OnboardFinish(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = c.Onboarding(ctx)
	if err != nil || !got.Done || !got.Harnesses[domain.HarnessCodex].Installed {
		t.Errorf("after finish = %+v, %v", got, err)
	}
	if !reflect.DeepEqual(f.installed, []domain.Harness{domain.HarnessCodex}) {
		t.Errorf("installed = %v", f.installed)
	}
}

func TestOnboardInstallRefusesAnUnknownHarnessAndSurfacesFailures(t *testing.T) {
	f := &fakeOnboarder{failOn: domain.HarnessClaude}
	_, path := start(t, &memStore{}, daemon.WithOnboarding(f))
	c := dial(t, path)
	ctx := context.Background()

	var rerr *rpc.Error
	if _, err := c.OnboardInstall(ctx, "vim"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("unknown harness: %v", err)
	}
	if _, err := c.OnboardInstall(ctx, domain.HarnessClaude); !errors.As(err, &rerr) || rerr.Code != rpc.CodeFailed {
		t.Errorf("failing install: %v", err)
	}
	if len(f.installed) != 0 {
		t.Errorf("installed = %v", f.installed)
	}
}

func TestOnboardingWithoutAnOnboarderIsUnavailable(t *testing.T) {
	_, path := start(t, &memStore{})
	var rerr *rpc.Error
	if _, err := dial(t, path).Onboarding(context.Background()); !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable {
		t.Errorf("err = %v", err)
	}
}

func TestOnboardNvimAddsThePluginSetup(t *testing.T) {
	f := &fakeOnboarder{state: domain.Onboarding{Nvim: domain.NvimSetup{OnPath: true, PluginFound: true}}}
	_, path := start(t, &memStore{}, daemon.WithOnboarding(f))
	n, err := dial(t, path).OnboardNvim(context.Background())
	if err != nil || !n.Configured || f.nvimInstalls != 1 {
		t.Errorf("nvim = %+v, %v, installs %d", n, err, f.nvimInstalls)
	}
}
