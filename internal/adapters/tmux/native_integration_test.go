//go:build integration

package tmux_test

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/tmux"
	"github.com/giovaniif/agent-workspace/internal/app"
)

type controlClient struct {
	stdin io.WriteCloser
	mu    sync.Mutex
	lines []string
}

func attachNative(t *testing.T, argv []string) *controlClient {
	t.Helper()
	if !slices.Contains(argv, "-C") {
		t.Fatalf("argv = %v; want a control-mode attach", argv)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &controlClient{stdin: stdin}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			c.mu.Lock()
			c.lines = append(c.lines, scanner.Text())
			c.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return c
}

func (c *controlClient) saw(prefix, contains string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.lines {
		if strings.HasPrefix(l, prefix) && strings.Contains(l, contains) {
			return true
		}
	}
	return false
}

func (c *controlClient) detach(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(c.stdin, "detach-client\n"); err != nil {
		t.Fatal(err)
	}
}

func tmuxOut(t *testing.T, argv []string, args ...string) string {
	t.Helper()
	full := append(slices.Clone(argv[:slices.Index(argv, "-C")]), args...)
	out, err := exec.Command(full[0], full[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v: %s", full, err, out)
	}
	return strings.TrimSpace(string(out))
}

func ticker(name string) app.PaneSpec {
	return app.PaneSpec{Name: name, Command: []string{"sh", "-c", "while :; do echo tick-" + name + "; sleep 0.1; done"}}
}

func TestNativeClientGetsOutputOfEveryParkedPaneAndNewWindows(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.Create(ctx, ticker("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Create(ctx, ticker("b"))
	if err != nil {
		t.Fatal(err)
	}
	shown, err := h.Create(ctx, ticker("shown"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, shown, slot); err != nil {
		t.Fatal(err)
	}

	native, err := h.OpenNative(ctx, 132, 40)
	if err != nil {
		t.Fatal(err)
	}
	if native.Session == "" || !slices.Contains(native.Argv, native.Session) && !slices.Contains(native.Argv, "="+native.Session) {
		t.Fatalf("native = %+v; want an argv attaching its session", native)
	}
	layout := func() string {
		return tmuxOut(t, native.Argv, "list-panes", "-t", string(slot), "-F", "#{pane_id} #{pane_width}x#{pane_height}")
	}
	before := layout()
	panes := map[app.PaneID]app.NativePane{}
	for _, p := range native.Panes {
		panes[p.Pane] = p
	}
	for _, want := range []app.PaneID{a, b} {
		p, ok := panes[want]
		if !ok || p.Window == "" || p.Cols == 0 || p.Rows == 0 {
			t.Fatalf("panes = %+v; want parked pane %s with its window and size", native.Panes, want)
		}
	}

	client := attachNative(t, native.Argv)
	if _, err := io.WriteString(client.stdin, "refresh-client -C 132x40\n"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []app.PaneID{a, b} {
		waitFor(t, "%output from "+string(p), func() bool { return client.saw("%output "+string(p)+" ", "tick-") })
	}

	c, err := h.Create(ctx, ticker("c"))
	if err != nil {
		t.Fatal(err)
	}
	window := tmuxOut(t, native.Argv, "display-message", "-p", "-t", string(c), "#{window_id}")
	waitFor(t, "%window-add "+window, func() bool { return client.saw("%window-add "+window, "") })
	waitFor(t, "%output from the new pane", func() bool { return client.saw("%output "+string(c)+" ", "tick-c") })

	if err := h.Show(ctx, a, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, shown, slot); err != nil {
		t.Fatal(err)
	}
	if got := layout(); got != before {
		t.Fatalf("TUI layout with a native client = %q; want %q", got, before)
	}
}

func TestNativeClientsGetTheirOwnSessionsWhichGoWhenTheyDetach(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if _, err := h.Create(ctx, catPane("a")); err != nil {
		t.Fatal(err)
	}
	first, err := h.OpenNative(ctx, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.OpenNative(ctx, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if first.Session == second.Session {
		t.Fatalf("both native clients got session %q", first.Session)
	}
	client := attachNative(t, first.Argv)
	waitFor(t, "the client to attach", func() bool { return client.saw("%session-changed", first.Session) })
	client.detach(t)
	waitFor(t, "the detached client's session to go", func() bool {
		sessions := tmuxOut(t, first.Argv, "list-sessions", "-F", "#{session_name}")
		return !slices.Contains(strings.Fields(sessions), first.Session)
	})
	sessions := strings.Fields(tmuxOut(t, first.Argv, "list-sessions", "-F", "#{session_name}"))
	if !slices.Contains(sessions, second.Session) || !slices.Contains(sessions, "agentws") {
		t.Fatalf("sessions = %v; want the other native session and agentws kept", sessions)
	}
}

func TestNativeClientShowsThePaneInTheTUISlotAcrossSwapsWithoutResizingTheTUI(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.Create(ctx, ticker("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Create(ctx, ticker("b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, a, slot); err != nil {
		t.Fatal(err)
	}
	native, err := h.OpenNative(ctx, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	var inSlot app.NativePane
	for _, p := range native.Panes {
		if p.Pane == a {
			inSlot = p
		}
	}
	if inSlot.Window != string(slot) || inSlot.Cols == 0 || inSlot.Rows == 0 {
		t.Fatalf("panes = %+v; want %s in the TUI window %s with its size", native.Panes, a, slot)
	}

	layout := func() string {
		return tmuxOut(t, native.Argv, "list-panes", "-t", string(slot), "-F", "#{pane_index} #{pane_width}x#{pane_height}")
	}
	before := layout()

	client := attachNative(t, native.Argv)
	if _, err := io.WriteString(client.stdin, "refresh-client -C 90x20\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "%output from the pane in the TUI slot", func() bool { return client.saw("%output "+string(a)+" ", "tick-a") })

	if err := h.Show(ctx, b, slot); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.lines = nil
	client.mu.Unlock()
	for _, p := range []app.PaneID{a, b} {
		waitFor(t, "%output from "+string(p)+" after the swap", func() bool { return client.saw("%output "+string(p)+" ", "tick-") })
	}
	if got := layout(); got != before {
		t.Fatalf("TUI layout with a smaller native client = %q; want %q", got, before)
	}
}

func TestTUIFollowsTerminalSizeBesideNative(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.Create(ctx, ticker("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, a, slot); err != nil {
		t.Fatal(err)
	}
	native, err := h.OpenNative(ctx, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	client := attachNative(t, native.Argv)
	if _, err := io.WriteString(client.stdin, "refresh-client -C 90x20\n"); err != nil {
		t.Fatal(err)
	}
	outer := outerTerminal(t, h, slot)
	size := func() string {
		return tmuxOut(t, native.Argv, "display-message", "-p", "-t", string(slot), "#{window_width}x#{window_height}")
	}
	sidebar := func() string {
		return tmuxOut(t, native.Argv, "display-message", "-p", "-t", string(slot)+".0", "#{pane_width}")
	}
	waitFor(t, "the TUI window to take its terminal's size", func() bool { return size() == "120x40" })
	outer("resize-window", "-t", "outer", "-x", "150", "-y", "45")
	waitFor(t, "the TUI window to follow its terminal's new size", func() bool { return size() == "150x45" })
	waitFor(t, "the sidebar to stay pinned", func() bool { return sidebar() == strconv.Itoa(tmux.SidebarWidth) })
}
