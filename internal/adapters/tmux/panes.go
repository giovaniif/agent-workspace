package tmux

import (
	"context"
	"sort"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/app"
)

func (h *Host) ensureSession(ctx context.Context) error {
	if _, err := h.run(ctx, "", "has-session", "-t", "="+sessionName); err == nil {
		return nil
	}
	_, err := h.run(ctx, "", "new-session", "-d", "-s", sessionName, "-n", "park", "-x", "200", "-y", "50", placeholder)
	if err != nil && strings.Contains(err.Error(), "duplicate session") {
		return nil
	}
	return err
}

func (h *Host) Create(ctx context.Context, spec app.PaneSpec) (app.PaneID, error) {
	id, err := h.newWindow(ctx, "pane-"+spec.Name, spec)
	if err != nil {
		return "", err
	}
	pane := app.PaneID(id.pane)
	if _, err := h.run(ctx, "", "set-option", "-p", "-t", id.pane, managedOption, "1"); err != nil {
		_ = h.Kill(ctx, pane)
		return "", err
	}
	_ = h.linkParked(ctx)
	return pane, nil
}

type newWindowResult struct{ window, pane string }

func (h *Host) newWindow(ctx context.Context, windowName string, spec app.PaneSpec) (newWindowResult, error) {
	if err := h.ensureSession(ctx); err != nil {
		return newWindowResult{}, err
	}
	args := []string{"new-window", "-d", "-P", "-F", "#{window_id} #{pane_id}", "-t", sessionName + ":", "-n", windowName}
	if spec.Dir != "" {
		args = append(args, "-c", spec.Dir)
	}
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	args = append(args, spec.Command...)
	out, err := h.run(ctx, "", args...)
	if err != nil {
		return newWindowResult{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return newWindowResult{}, &tmuxError{args: args, stderr: "unexpected output " + out}
	}
	return newWindowResult{window: fields[0], pane: fields[1]}, nil
}

func (h *Host) Kill(ctx context.Context, pane app.PaneID) error {
	_, err := h.run(ctx, "", "kill-pane", "-t", string(pane))
	if isMissingTarget(err) || isServerGone(err) {
		return nil
	}
	return err
}

func (h *Host) List(ctx context.Context) ([]app.PaneInfo, error) {
	out, err := h.run(ctx, "", "list-panes", "-a", "-F", "#{pane_id} #{pane_dead} #{"+managedOption+"}")
	if isServerGone(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parsePanes(out), nil
}

func (h *Host) Alive(ctx context.Context, pane app.PaneID) (bool, error) {
	out, err := h.run(ctx, "", "display-message", "-p", "-t", string(pane), "#{pane_dead}")
	if isMissingTarget(err) || isServerGone(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "0", nil
}

func parsePanes(out string) []app.PaneInfo {
	var panes []app.PaneInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "1" {
			continue
		}
		panes = append(panes, app.PaneInfo{ID: app.PaneID(fields[0]), Alive: fields[1] == "0"})
	}
	return panes
}

func (h *Host) SetTitle(ctx context.Context, pane app.PaneID, title string) error {
	if err := h.turnTitlesOn(ctx); err != nil {
		return err
	}
	_, err := h.run(ctx, "", "set-option", "-p", "-t", string(pane), "@agentws_title", title)
	return err
}

func (h *Host) turnTitlesOn(ctx context.Context) error {
	h.titlesMu.Lock()
	defer h.titlesMu.Unlock()
	if h.titlesOn {
		return nil
	}
	if _, err := h.run(ctx, "", "set-option", "-g", "pane-border-status", "top"); err != nil {
		return err
	}
	if _, err := h.run(ctx, "", "set-option", "-g", "pane-border-format", titleFormat); err != nil {
		return err
	}
	h.titlesOn = true
	return nil
}
