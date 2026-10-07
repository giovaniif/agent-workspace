package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
)

const belowPaneIndex = "2"

const ShellHeight = "35%"

const popupSessionPrefix = "agentws-popup-"

func (h *Host) BelowPane(ctx context.Context, slot app.Slot) app.PaneID {
	out, err := h.run(ctx, "", "list-panes", "-t", string(slot), "-F", "#{pane_index} #{pane_id}")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if index, id, ok := strings.Cut(strings.TrimSpace(line), " "); ok && index == belowPaneIndex {
			return app.PaneID(id)
		}
	}
	return ""
}

func (h *Host) ShowBelow(ctx context.Context, pane app.PaneID, slot app.Slot) error {
	switch h.BelowPane(ctx, slot) {
	case pane:
		return nil
	case "":
	default:
		if err := h.HideBelow(ctx, slot); err != nil {
			return err
		}
	}
	_, err := h.run(ctx, "", "join-pane", "-d", "-v", "-l", ShellHeight, "-s", string(pane), "-t", string(slot)+"."+slotPaneIndex)
	return err
}

func (h *Host) FocusBelow(ctx context.Context, slot app.Slot) error {
	below := h.BelowPane(ctx, slot)
	if below == "" {
		return errors.New("no shell is shown below the agent pane")
	}
	_, err := h.run(ctx, "", "select-pane", "-t", string(below))
	return err
}

func (h *Host) HideBelow(ctx context.Context, slot app.Slot) error {
	below := h.BelowPane(ctx, slot)
	if below == "" {
		return nil
	}
	if _, err := h.run(ctx, "", "break-pane", "-d", "-s", string(below), "-n", "pane-parked"); err != nil {
		return err
	}
	_ = h.linkNative(ctx)
	return nil
}

func (h *Host) Popup(ctx context.Context, pane app.PaneID) error {
	window, err := h.parkedWindow(ctx, pane)
	if err != nil {
		return err
	}
	client, err := h.popupClient(ctx)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s%d-%d", popupSessionPrefix, os.Getpid(), h.bufferSeq.Add(1))
	inner := strings.Join([]string{
		"env", "-u", "TMUX", "tmux", "-L", shellQuote(h.socket), "-f", shellQuote(h.configPath),
		"new-session", "-t", sessionName, "-s", name,
		`\;`, "select-window", "-t", window,
		`\;`, "set-option", "destroy-unattached", "on",
	}, " ")
	cmd := exec.Command("tmux", "-L", h.socket, "-f", h.configPath, "display-popup", "-c", client, "-E", "-w", "80%", "-h", "80%", inner)
	cmd.Env = withoutTmuxEnv(os.Environ())
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

const (
	popupMaxWidth  = 100
	popupMaxHeight = 32
)

func (h *Host) PopupCommand(ctx context.Context, spec app.PaneSpec) error {
	client, err := h.popupClient(ctx)
	if err != nil {
		return err
	}
	size, err := h.run(ctx, "", "display-message", "-c", client, "-p", "#{client_width} #{client_height}")
	if err != nil {
		return err
	}
	var cw, ch int
	if _, err := fmt.Sscanf(size, "%d %d", &cw, &ch); err != nil {
		return err
	}
	w, hgt := min(popupMaxWidth, cw-2), min(popupMaxHeight, ch-2)
	args := []string{"-L", h.socket, "-f", h.configPath, "display-popup", "-c", client, "-E", "-w", strconv.Itoa(w), "-h", strconv.Itoa(hgt)}
	if spec.Dir != "" {
		args = append(args, "-d", spec.Dir)
	}
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	quoted := make([]string, len(spec.Command))
	for i, a := range spec.Command {
		quoted[i] = shellQuote(a)
	}
	cmd := exec.Command("tmux", append(args, strings.Join(quoted, " "))...)
	cmd.Env = withoutTmuxEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return &tmuxError{args: []string{"display-popup"}, stderr: strings.TrimSpace(stderr.String()), err: err}
		}
	case <-time.After(popupLaunchGrace):
	}
	return nil
}

const popupLaunchGrace = 300 * time.Millisecond

func (h *Host) parkedWindow(ctx context.Context, pane app.PaneID) (string, error) {
	out, err := h.run(ctx, "", "display-message", "-p", "-t", string(pane), "#{window_id} #{window_panes}")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return "", &tmuxError{args: []string{"display-message"}, stderr: "unexpected output " + out}
	}
	if fields[1] == "1" {
		return fields[0], nil
	}
	parked, err := h.run(ctx, "", "break-pane", "-d", "-P", "-F", "#{window_id}", "-s", string(pane), "-n", "pane-parked")
	return strings.TrimSpace(parked), err
}

func (h *Host) popupClient(ctx context.Context) (string, error) {
	out, err := h.run(ctx, "", "list-clients", "-F", "#{client_name} #{session_name} #{client_activity}")
	if err != nil && !isServerGone(err) {
		return "", err
	}
	if client, ok := activeClient(out); ok {
		return client, nil
	}
	return "", errors.New("no terminal is attached to the agentws tmux server")
}

func activeClient(out string) (string, bool) {
	client, newest := "", int64(-1)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasPrefix(fields[1], popupSessionPrefix) {
			continue
		}
		if n, err := strconv.ParseInt(fields[2], 10, 64); err == nil && n > newest {
			client, newest = fields[0], n
		}
	}
	return client, client != ""
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
