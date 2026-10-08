package rpc

const (
	MethodTabNew   = "tab.new"
	MethodTabShow  = "tab.show"
	MethodTabStep  = "tab.step"
	MethodTabClose = "tab.close"
)

type TabParams struct {
	Session string `json:"session"`
	Tab     string `json:"tab,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Delta   int    `json:"delta,omitempty"`
}

type ActiveTab struct {
	Worktree string `json:"worktree"`
	Tab      string `json:"tab"`
}
