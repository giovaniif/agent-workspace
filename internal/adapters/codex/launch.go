package codex

import (
	"context"
	"fmt"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type LaunchRequest struct {
	Name   string
	Dir    string
	Model  string
	Effort string
	Prompt string
	Resume string
}

func LaunchSpec(req LaunchRequest) app.PaneSpec {
	return launchSpec("codex", req)
}

func launchSpec(bin string, req LaunchRequest) app.PaneSpec {
	command := []string{bin}
	if req.Resume != "" {
		command = append(command, "resume")
	}
	if req.Model != "" {
		command = append(command, "--model", req.Model)
	}
	if req.Effort != "" {
		command = append(command, "-c", fmt.Sprintf("model_reasoning_effort=%q", req.Effort))
	}
	if req.Prompt != "" {
		command = append(command, "--", req.Prompt)
	}
	if req.Resume != "" {
		command = append(command, req.Resume)
	}
	return app.PaneSpec{Name: req.Name, Dir: req.Dir, Command: command}
}

func Launch(ctx context.Context, host app.TerminalHost, req LaunchRequest) (app.PaneID, error) {
	return host.Create(ctx, LaunchSpec(req))
}

type Adapter struct {
	Binary string
}

func (Adapter) Harness() domain.Harness { return domain.HarnessCodex }

func (a Adapter) Launch(req app.LaunchRequest) app.PaneSpec {
	bin := a.Binary
	if bin == "" {
		bin = "codex"
	}
	return launchSpec(bin, LaunchRequest(req))
}
