package app

import "github.com/giovaniif/crew/internal/domain"

type LaunchRequest struct {
	Name   string
	Dir    string
	Model  string
	Effort string
	Prompt string
	Resume string
}

type HarnessAdapter interface {
	Harness() domain.Harness
	Launch(req LaunchRequest) PaneSpec
}
