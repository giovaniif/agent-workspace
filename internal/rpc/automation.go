package rpc

import "github.com/giovaniif/agent-workspace/internal/domain"

const (
	MethodAutomationList = "automation.list"
	MethodAutomationShow = "automation.show"
	MethodAutomationRun  = "automation.run"
	MethodAutomationRuns = "automation.runs"
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

type AutomationRunParams struct {
	Name     string `json:"name,omitempty"`
	Context  string `json:"context,omitempty"`
	WorkItem string `json:"work_item,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Rerun    string `json:"rerun,omitempty"`
}

type AutomationRunResult struct {
	Run     domain.AutomationRun `json:"run"`
	Session domain.Session       `json:"session"`
}

type AutomationRunsParams struct {
	Name  string `json:"name,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type AutomationRuns struct {
	Runs []domain.AutomationRun `json:"runs"`
}
