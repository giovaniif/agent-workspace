package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	DefaultSocket = "agentws"
	sessionName   = "agentws"
	managedOption = "@agentws"
	placeholder   = "tail -f /dev/null"
	emptyState    = `printf 'No session in view.\n\nPress n to start one.\n'; exec tail -f /dev/null`

	FocusSidebarKey = `C-\`
)

const titleFormat = "#{?@agentws_title, #{q/h:@agentws_title} ,}"

const configContents = `set -g status off
set -g prefix None
unbind-key -a
bind-key -n C-\\ select-pane -t :.0
set -g escape-time 0
set -g remain-on-exit off
set -g history-limit 50000
set -g default-terminal "tmux-256color"
set -g pane-border-status top
` + "set -g pane-border-format \"" + titleFormat + "\"\n" + `bind -n M-t if -F '#{m:agentws-popup-*,#{session_name}}' 'detach-client' 'send-keys M-t'
`

const mouseOn = `set -g mouse on
bind -n MouseDown1Pane select-pane -t = \; send -M
bind -n MouseDrag1Border resize-pane -M
bind -n MouseDrag1Pane if -F '#{||:#{pane_in_mode},#{mouse_any_flag}}' 'send -M' 'copy-mode -M'
bind -n WheelUpPane if -F '#{||:#{pane_in_mode},#{mouse_any_flag}}' 'send -M' 'copy-mode -e'
bind -n WheelDownPane if -F '#{||:#{pane_in_mode},#{mouse_any_flag}}' 'send -M'
bind -T copy-mode WheelUpPane select-pane \; send -X -N 5 scroll-up
bind -T copy-mode WheelDownPane select-pane \; send -X -N 5 scroll-down
bind -T copy-mode MouseDown1Pane select-pane \; send -X clear-selection
bind -T copy-mode MouseDrag1Pane select-pane \; send -X begin-selection
bind -T copy-mode MouseDragEnd1Pane send -X copy-selection-and-cancel
bind -T copy-mode q send -X cancel
bind -T copy-mode Escape send -X cancel
`

const mouseOff = "set -g mouse off\n"

func config(noMouse bool) string {
	if noMouse {
		return configContents + mouseOff
	}
	return configContents + mouseOn
}

type Config struct {
	Socket     string
	ConfigPath string
	NoMouse    bool
}

type Host struct {
	socket     string
	configPath string
	noMouse    bool

	configOnce sync.Once
	configErr  error
	loadMu     sync.Mutex
	loaded     bool
	titlesMu   sync.Mutex
	titlesOn   bool
	bufferSeq  atomic.Uint64
	nativeMu   sync.Mutex
}

func New(cfg Config) *Host {
	socket := cfg.Socket
	if socket == "" {
		socket = DefaultSocket
	}
	return &Host{socket: socket, configPath: cfg.ConfigPath, noMouse: cfg.NoMouse}
}

func (h *Host) Close(ctx context.Context) error {
	_, err := h.run(ctx, "", "kill-server")
	if isServerGone(err) {
		return nil
	}
	return err
}

func (h *Host) ShowOption(ctx context.Context, name string) (string, error) {
	out, err := h.run(ctx, "", "show-options", "-gv", name)
	return strings.TrimSpace(out), err
}

func (h *Host) run(ctx context.Context, stdin string, args ...string) (string, error) {
	if err := h.ensureConfig(); err != nil {
		return "", err
	}
	h.loadConfig(ctx)
	return h.invoke(ctx, stdin, args...)
}

func (h *Host) loadConfig(ctx context.Context) {
	h.loadMu.Lock()
	defer h.loadMu.Unlock()
	if h.loaded {
		return
	}
	_, _ = h.invoke(ctx, "", "source-file", h.configPath)
	h.loaded = ctx.Err() == nil
}

func (h *Host) invoke(ctx context.Context, stdin string, args ...string) (string, error) {
	full := append([]string{"-L", h.socket, "-f", h.configPath}, args...)
	cmd := exec.CommandContext(ctx, "tmux", full...)
	cmd.Env = withoutTmuxEnv(os.Environ())
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &tmuxError{args: args, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return stdout.String(), nil
}

func (h *Host) ensureConfig() error {
	h.configOnce.Do(func() {
		if h.configPath == "" {
			h.configErr = errors.New("tmux: Config.ConfigPath is required")
			return
		}
		if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
			h.configErr = err
			return
		}
		h.configErr = os.WriteFile(h.configPath, []byte(config(h.noMouse)), 0o644)
	})
	return h.configErr
}

func withoutTmuxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			out = append(out, kv)
		}
	}
	return out
}

type tmuxError struct {
	args   []string
	stderr string
	err    error
}

func (e *tmuxError) Error() string {
	return fmt.Sprintf("tmux %s: %s (%v)", strings.Join(e.args, " "), e.stderr, e.err)
}

func (e *tmuxError) Unwrap() error { return e.err }

func isServerGone(err error) bool {
	var te *tmuxError
	if !errors.As(err, &te) {
		return false
	}
	return strings.Contains(te.stderr, "no server running") ||
		strings.Contains(te.stderr, "error connecting") ||
		strings.Contains(te.stderr, "server exited")
}

func isMissingTarget(err error) bool {
	var te *tmuxError
	return errors.As(err, &te) && strings.Contains(te.stderr, "can't find")
}
