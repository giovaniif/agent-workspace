package tmux

import (
	"context"
	"strconv"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/app"
)

const slotPaneIndex = "1"

const SidebarWidth = 48

func (h *Host) OpenClient(ctx context.Context, name string, tui app.PaneSpec) (app.Slot, error) {
	id, err := h.newWindow(ctx, clientWindowPrefix+name, tui)
	if err != nil {
		return "", err
	}
	slot := app.Slot(id.window)
	if err := h.sizeByTerminalOnly(ctx, slot); err != nil {
		return "", err
	}
	if err := h.addSlotPane(ctx, slot); err != nil {
		return "", err
	}
	if err := h.pinSidebar(ctx, slot, strconv.Itoa(SidebarWidth)); err != nil {
		return "", err
	}
	_ = h.linkNative(ctx)
	return slot, nil
}

const resizeToClient = "run-shell -C 'resize-window -t #{window_id} -x #{client_width} -y #{client_height} ; set-hook -R -w -t #{window_id} window-resized'"

func (h *Host) sizeByTerminalOnly(ctx context.Context, slot app.Slot) error {
	if _, err := h.run(ctx, "", "set-option", "-w", "-t", string(slot), "window-size", "manual"); err != nil {
		return err
	}
	for _, hook := range []string{"client-attached", "client-resized"} {
		if _, err := h.run(ctx, "", "set-hook", "-t", sessionName, hook, resizeToClient); err != nil {
			return err
		}
	}
	return nil
}

const ReviewWidth = "75%"

func (h *Host) WidenSidebar(ctx context.Context, slot app.Slot, wide bool) error {
	width := strconv.Itoa(SidebarWidth)
	if wide {
		width = ReviewWidth
	}
	return h.pinSidebar(ctx, slot, width)
}

func (h *Host) pinSidebar(ctx context.Context, slot app.Slot, width string) error {
	target := string(slot) + ".0"
	if _, err := h.run(ctx, "", "resize-pane", "-t", target, "-x", width); err != nil {
		return err
	}
	_, err := h.run(ctx, "", "set-hook", "-w", "-t", string(slot), "window-resized", "resize-pane -t "+target+" -x "+width)
	return err
}

func (h *Host) addSlotPane(ctx context.Context, slot app.Slot) error {
	_, err := h.run(ctx, "", "split-window", "-h", "-d", "-l", "70%", "-t", string(slot)+".0", emptyState)
	return err
}

func (h *Host) EnsureSlot(ctx context.Context, slot app.Slot) error {
	count, err := h.paneCount(ctx, slot)
	if err != nil || count != 1 {
		return err
	}
	return h.addSlotPane(ctx, slot)
}

func (h *Host) Show(ctx context.Context, pane app.PaneID, slot app.Slot) error {
	err := h.swapIntoSlot(ctx, pane, slot)
	if err == nil {
		return nil
	}
	count, countErr := h.paneCount(ctx, slot)
	if countErr != nil || count != 1 {
		return err
	}
	if addErr := h.addSlotPane(ctx, slot); addErr != nil {
		return addErr
	}
	return h.swapIntoSlot(ctx, pane, slot)
}

func (h *Host) swapIntoSlot(ctx context.Context, pane app.PaneID, slot app.Slot) error {
	_, err := h.run(ctx, "", "swap-pane", "-d", "-s", string(pane), "-t", string(slot)+"."+slotPaneIndex)
	return err
}

func (h *Host) SlotHasPane(ctx context.Context, slot app.Slot) bool {
	count, err := h.paneCount(ctx, slot)
	return err != nil || count > 1
}

func (h *Host) paneCount(ctx context.Context, slot app.Slot) (int, error) {
	out, err := h.run(ctx, "", "list-panes", "-t", string(slot), "-F", "#{pane_id}")
	if err != nil {
		return 0, err
	}
	return len(strings.Fields(out)), nil
}

func (h *Host) ClientOpen(ctx context.Context, slot app.Slot) bool {
	out, err := h.run(ctx, "", "display-message", "-p", "-t", string(slot), "#{window_id}")
	return err == nil && strings.TrimSpace(out) == string(slot)
}

func (h *Host) FocusSlot(ctx context.Context, slot app.Slot) error {
	_, err := h.run(ctx, "", "select-pane", "-t", string(slot)+"."+slotPaneIndex)
	return err
}

func (h *Host) AttachCommand(slot app.Slot) []string {
	return []string{"tmux", "-L", h.socket, "-f", h.configPath, "attach-session", "-t", string(slot)}
}

func (h *Host) ShownIn(ctx context.Context, slot app.Slot) app.PaneID {
	out, err := h.run(ctx, "", "display-message", "-p", "-t", string(slot)+"."+slotPaneIndex, "#{pane_id}")
	if err != nil {
		return ""
	}
	return app.PaneID(strings.TrimSpace(out))
}

func (h *Host) Detach(ctx context.Context, slot app.Slot) error {
	session, err := h.run(ctx, "", "display-message", "-p", "-t", string(slot), "#{session_name}")
	if err != nil {
		return err
	}
	_, err = h.run(ctx, "", "detach-client", "-s", strings.TrimSpace(session))
	return err
}
