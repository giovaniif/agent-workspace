package omp

import (
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type Adapter struct {
	Binary string
}

func (Adapter) Harness() domain.Harness { return domain.HarnessOmp }

func (a Adapter) Launch(req app.LaunchRequest) app.PaneSpec {
	command := []string{a.Binary}
	if a.Binary == "" {
		command[0] = "omp"
	}
	// why: a bare --resume opens omp's session picker instead of a session.
	if req.Resume != "" {
		command = append(command, "--resume", req.Resume)
	}
	if req.Model != "" {
		command = append(command, "--model", req.Model)
	}
	if req.Effort != "" {
		command = append(command, "--thinking", req.Effort)
	}
	if req.Prompt != "" {
		// why: the separator keeps a prompt that starts with "-" from parsing as a flag.
		command = append(command, "--", req.Prompt)
	}
	return app.PaneSpec{Name: req.Name, Dir: req.Dir, Command: command}
}
