package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var (
	_ app.Notifier   = Osascript{}
	_ app.Foreground = Osascript{}
)

type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if line, _, _ := strings.Cut(strings.TrimSpace(string(exit.Stderr)), "\n"); line != "" {
			err = fmt.Errorf("%s: %w: %s", name, err, line)
		}
	}
	return out, err
}

type Osascript struct {
	Run Runner
}

func New() Osascript { return Osascript{Run: execRunner} }

var bannerScript = []string{
	"on run argv",
	"if item 3 of argv is \"\" then",
	"display notification (item 2 of argv) with title (item 1 of argv)",
	"else",
	"display notification (item 2 of argv) with title (item 1 of argv) sound name (item 3 of argv)",
	"end if",
	"end run",
}

func (o Osascript) Notify(ctx context.Context, b domain.Banner) error {
	args := make([]string, 0, 2*len(bannerScript)+3)
	for _, line := range bannerScript {
		args = append(args, "-e", line)
	}
	args = append(args, b.Title, b.Body, b.Sound)
	_, err := o.Run(ctx, "osascript", args...)
	return err
}

const frontmostScript = `tell application "System Events" to get name of first application process whose frontmost is true`

var terminalApps = map[string]bool{
	"terminal": true, "iterm2": true, "ghostty": true, "wezterm": true, "wezterm-gui": true,
	"kitty": true, "alacritty": true, "hyper": true, "warp": true,
}

func (o Osascript) TerminalFrontmost(ctx context.Context) bool {
	out, err := o.Run(ctx, "osascript", "-e", frontmostScript)
	return err == nil && terminalApps[strings.ToLower(strings.TrimSpace(string(out)))]
}

func LoadSounds(path string) (map[domain.AgentState]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Sounds map[string]string `json:"sounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	out := map[domain.AgentState]string{}
	for _, st := range []domain.AgentState{domain.StatePermission, domain.StateWaiting, domain.StateDone} {
		if name := cfg.Sounds[string(st)]; name != "" {
			out[st] = name
		}
	}
	return out, nil
}

func (Osascript) Remove(context.Context, string) error { return nil }
