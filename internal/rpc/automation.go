package rpc

import "github.com/giovaniif/agent-workspace/internal/domain"

const (
	MethodAutomationList = "automation.list"
	MethodAutomationShow = "automation.show"
)

type Automation = domain.Automation

type AutomationListParams struct {
	Repo string `json:"repo,omitempty"`
}

type AutomationList struct {
	Automations []Automation `json:"automations"`
	Problems    []string     `json:"problems,omitempty"`
}

type AutomationShowParams struct {
	Name string `json:"name"`
	Repo string `json:"repo,omitempty"`
}
