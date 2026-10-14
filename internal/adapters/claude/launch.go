package claude

import (
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type Adapter struct {
	Binary string
}

func (Adapter) Harness() domain.Harness { return domain.HarnessClaude }

func (a Adapter) Launch(req app.LaunchRequest) app.PaneSpec {
	bin := a.Binary
	if bin == "" {
		bin = "claude"
	}
	command := []string{bin}
	if req.Resume != "" {
		command = append(command, "--resume", req.Resume)
	}
	if req.Model != "" {
		command = append(command, "--model", req.Model)
	}
	if req.Effort != "" {
		command = append(command, "--effort", req.Effort)
	}
	if req.Prompt != "" {
		command = append(command, "--", req.Prompt)
	}
	return app.PaneSpec{Name: req.Name, Dir: req.Dir, Command: command}
}
