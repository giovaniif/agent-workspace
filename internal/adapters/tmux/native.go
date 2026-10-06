package tmux

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
)

const (
	nativeSessionPrefix = "agentws-native-"
	nativeWindowName    = "native"
	parkedWindowPrefix  = "pane-"
	nativeUnattachedTTL = time.Minute
)

const nativePaneFormat = "#{window_id}\t#{pane_id}\t#{" + managedOption + "}\t#{pane_width}\t#{pane_height}\t" +
	"#{mouse_any_flag}\t#{mouse_button_flag}\t#{mouse_standard_flag}\t#{mouse_sgr_flag}\t" +
	"#{alternate_on}\t#{cursor_flag}\t#{keypad_cursor_flag}"

func (h *Host) OpenNative(ctx context.Context, cols, rows int) (app.NativeClient, error) {
	if err := h.ensureSession(ctx); err != nil {
		return app.NativeClient{}, err
	}
	h.reapNative(ctx, time.Now())
	name := fmt.Sprintf("%s%d-%d", nativeSessionPrefix, os.Getpid(), h.bufferSeq.Add(1))
	if _, err := h.run(ctx, "", "new-session", "-d", "-s", name, "-n", nativeWindowName,
		"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), placeholder); err != nil {
		return app.NativeClient{}, err
	}
	if _, err := h.run(ctx, "", "set-hook", "-t", name, "client-attached",
		"set-option -t "+name+" destroy-unattached on"); err != nil {
		h.killSession(ctx, name)
		return app.NativeClient{}, err
	}
	if err := h.linkParked(ctx); err != nil {
		h.killSession(ctx, name)
		return app.NativeClient{}, err
	}
	out, err := h.run(ctx, "", "list-panes", "-s", "-t", "="+name, "-F", nativePaneFormat)
	if err != nil {
		h.killSession(ctx, name)
		return app.NativeClient{}, err
	}
	return app.NativeClient{
		Session: name,
		Argv:    []string{"tmux", "-L", h.socket, "-f", h.configPath, "-C", "attach-session", "-t", "=" + name},
		Panes:   parseNativePanes(out),
	}, nil
}

func (h *Host) killSession(ctx context.Context, name string) {
	_, _ = h.run(ctx, "", "kill-session", "-t", "="+name)
}

func (h *Host) reapNative(ctx context.Context, now time.Time) {
	out, err := h.run(ctx, "", "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{session_created}")
	if err != nil {
		return
	}
	for _, name := range staleNative(out, now) {
		h.killSession(ctx, name)
	}
}

func staleNative(out string, now time.Time) []string {
	var stale []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || !strings.HasPrefix(fields[0], nativeSessionPrefix) || fields[1] != "0" {
			continue
		}
		created, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || now.Sub(time.Unix(created, 0)) < nativeUnattachedTTL {
			continue
		}
		stale = append(stale, fields[0])
	}
	return stale
}

func (h *Host) linkParked(ctx context.Context) error {
	h.nativeMu.Lock()
	defer h.nativeMu.Unlock()
	out, err := h.run(ctx, "", "list-windows", "-a", "-F", "#{session_name}\t#{window_id}\t#{window_name}")
	if err != nil {
		return err
	}
	for _, l := range missingLinks(out) {
		if _, err := h.run(ctx, "", "link-window", "-d", "-s", l.window, "-t", "="+l.session+":"); err != nil && !isMissingTarget(err) {
			return err
		}
	}
	return nil
}

type windowLink struct{ window, session string }

func missingLinks(out string) []windowLink {
	var parked []string
	var natives []string
	has := map[windowLink]bool{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		session, window, name := fields[0], fields[1], fields[2]
		has[windowLink{window, session}] = true
		if strings.HasPrefix(session, nativeSessionPrefix) && !seen[session] {
			seen[session] = true
			natives = append(natives, session)
		}
		if session == sessionName && strings.HasPrefix(name, parkedWindowPrefix) {
			parked = append(parked, window)
		}
	}
	var links []windowLink
	for _, session := range natives {
		for _, window := range parked {
			if l := (windowLink{window, session}); !has[l] {
				links = append(links, l)
			}
		}
	}
	return links
}

func parseNativePanes(out string) []app.NativePane {
	var panes []app.NativePane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 12 || f[2] != "1" {
			continue
		}
		cols, _ := strconv.Atoi(f[3])
		rows, _ := strconv.Atoi(f[4])
		panes = append(panes, app.NativePane{
			Pane:          app.PaneID(f[1]),
			Window:        f[0],
			Cols:          cols,
			Rows:          rows,
			MouseAny:      f[5] == "1",
			MouseButton:   f[6] == "1",
			MouseStandard: f[7] == "1",
			MouseSGR:      f[8] == "1",
			Alternate:     f[9] == "1",
			CursorVisible: f[10] == "1",
			CursorKeys:    f[11] == "1",
		})
	}
	return panes
}
