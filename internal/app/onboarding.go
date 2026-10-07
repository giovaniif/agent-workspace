package app

import (
	"context"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type Onboarder interface {
	Onboarding(ctx context.Context) (domain.Onboarding, error)
	Install(ctx context.Context, h domain.Harness) (domain.HarnessSetup, error)
	InstallNvim(ctx context.Context) (domain.NvimSetup, error)
	Remove(ctx context.Context, h domain.Harness) (domain.HarnessSetup, error)
	Finish(ctx context.Context) error
}
