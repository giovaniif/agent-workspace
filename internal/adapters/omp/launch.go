package omp

import (
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
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
		command = append(command, "--", req.Prompt)
	}
	return app.PaneSpec{Name: req.Name, Dir: req.Dir, Command: command}
}
