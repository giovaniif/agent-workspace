package app

import "context"

type PaneID string

type Slot string

type PaneSpec struct {
	Name    string
	Dir     string
	Command []string
	Env     map[string]string
}

type PaneInfo struct {
	ID    PaneID
	Alive bool
}

type NativePane struct {
	Pane          PaneID
	Window        string
	Cols          int
	Rows          int
	MouseAny      bool
	MouseButton   bool
	MouseStandard bool
	MouseSGR      bool
	Alternate     bool
	CursorVisible bool
	CursorKeys    bool
}

type NativeClient struct {
	Session string
	Argv    []string
	Panes   []NativePane
}

type TerminalHost interface {
	Create(ctx context.Context, spec PaneSpec) (PaneID, error)
	Kill(ctx context.Context, pane PaneID) error
	List(ctx context.Context) ([]PaneInfo, error)
	OpenClient(ctx context.Context, name string, tui PaneSpec) (Slot, error)
	Show(ctx context.Context, pane PaneID, slot Slot) error
	SendText(ctx context.Context, pane PaneID, text string, bracketedPaste bool) error
	SendKeys(ctx context.Context, pane PaneID, keys ...string) error
	Capture(ctx context.Context, pane PaneID, lines int) (string, error)
	Alive(ctx context.Context, pane PaneID) (bool, error)
	SetTitle(ctx context.Context, pane PaneID, title string) error
}

type Editor interface {
	Eval(ctx context.Context, socket, expr string) error
	Installed() bool
}
