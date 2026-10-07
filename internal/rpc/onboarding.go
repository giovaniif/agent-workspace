package rpc

import (
	"context"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const (
	MethodOnboarding        = "onboarding.status"
	MethodOnboardingInstall = "onboarding.install"
	MethodOnboardingFinish  = "onboarding.finish"
	MethodOnboardingNvim    = "onboarding.nvim"
	MethodOnboardingRemove  = "onboarding.remove"
)

type OnboardInstallParams struct {
	Harness domain.Harness `json:"harness"`
}

func (c *Client) Onboarding(ctx context.Context) (domain.Onboarding, error) {
	var out domain.Onboarding
	err := c.Call(ctx, MethodOnboarding, nil, &out)
	return out, err
}

func (c *Client) OnboardInstall(ctx context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	var out domain.HarnessSetup
	err := c.Call(ctx, MethodOnboardingInstall, OnboardInstallParams{Harness: h}, &out)
	return out, err
}

func (c *Client) OnboardRemove(ctx context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	var out domain.HarnessSetup
	err := c.Call(ctx, MethodOnboardingRemove, OnboardInstallParams{Harness: h}, &out)
	return out, err
}

func (c *Client) OnboardFinish(ctx context.Context) error {
	return c.Call(ctx, MethodOnboardingFinish, nil, nil)
}

func (c *Client) OnboardNvim(ctx context.Context) (domain.NvimSetup, error) {
	var out domain.NvimSetup
	err := c.Call(ctx, MethodOnboardingNvim, nil, &out)
	return out, err
}
