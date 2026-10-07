package daemon

import (
	"encoding/json"
	"slices"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func WithOnboarding(o app.Onboarder) Option {
	return func(d *Daemon) { d.onboard = o }
}

func (d *Daemon) onboarding(req rpc.Request) *rpc.Response {
	if d.onboard == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "onboarding is not configured")
	}
	ctx := d.ws.ctx
	switch req.Method {
	case rpc.MethodOnboardingInstall, rpc.MethodOnboardingRemove:
		var p rpc.OnboardInstallParams
		if err := json.Unmarshal(req.Params, &p); err != nil || !slices.Contains(domain.Harnesses(), p.Harness) {
			return errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" needs a harness: claude, codex or omp")
		}
		change := d.onboard.Install
		if req.Method == rpc.MethodOnboardingRemove {
			change = d.onboard.Remove
		}
		s, err := change(ctx, p.Harness)
		if err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, s)
	case rpc.MethodOnboardingNvim:
		n, err := d.onboard.InstallNvim(ctx)
		if err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, n)
	case rpc.MethodOnboardingFinish:
		if err := d.onboard.Finish(ctx); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		return result(req.ID, struct{}{})
	}
	o, err := d.onboard.Onboarding(ctx)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	return result(req.ID, o)
}
