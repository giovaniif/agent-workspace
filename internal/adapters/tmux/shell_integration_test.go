//go:build integration

package tmux_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
)

func tmuxIn(t testing.TB, h *tmux.Host, slot app.Slot, args ...string) string {
	t.Helper()
	attach := h.AttachCommand(slot)
	full := append(slices.Clone(attach[1:len(attach)-3]), args...)
	out, err := exec.Command(attach[0], full...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestShellSplitShowsBelowTheAgentPaneAndParksAgain(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := h.Create(ctx, catPane("agent"))
	shell, err := h.Create(ctx, catPane("shell"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, agent, slot); err != nil {
		t.Fatal(err)
	}
	if got := h.BelowPane(ctx, slot); got != "" {
		t.Fatalf("BelowPane = %q before any shell is shown", got)
	}

	if err := h.ShowBelow(ctx, shell, slot); err != nil {
		t.Fatal(err)
	}
	if got := h.BelowPane(ctx, slot); got != shell {
		t.Fatalf("BelowPane = %q after ShowBelow; want %q", got, shell)
	}
	if got := h.ShownIn(ctx, slot); got != agent {
		t.Fatalf("the agent pane is %q after ShowBelow; want %q to stay in the slot", got, agent)
	}
	sidebar := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot)+".0", "#{pane_id}")
	if active := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot), "#{pane_id}"); active != sidebar {
		t.Fatalf("active pane = %s after ShowBelow; want focus left on the sidebar %s", active, sidebar)
	}
	if err := h.FocusBelow(ctx, slot); err != nil {
		t.Fatal(err)
	}
	if active := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot), "#{pane_id}"); active != string(shell) {
		t.Fatalf("active pane = %s after FocusBelow; want the shell %s", active, shell)
	}

	if err := h.HideBelow(ctx, slot); err != nil {
		t.Fatal(err)
	}
	if got := h.BelowPane(ctx, slot); got != "" {
		t.Fatalf("BelowPane = %q after HideBelow", got)
	}
	if alive, _ := h.Alive(ctx, shell); !alive {
		t.Fatal("the shell died when it was hidden; it should be kept alive")
	}
	if err := h.ShowBelow(ctx, shell, slot); err != nil {
		t.Fatalf("showing the parked shell again: %v", err)
	}
}

func TestShowBelowReplacesTheShellAlreadyThere(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	agent, _ := h.Create(ctx, catPane("agent"))
	first, _ := h.Create(ctx, catPane("shell-1"))
	second, _ := h.Create(ctx, catPane("shell-2"))
	if err := h.Show(ctx, agent, slot); err != nil {
		t.Fatal(err)
	}
	for _, p := range []app.PaneID{first, second} {
		if err := h.ShowBelow(ctx, p, slot); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.BelowPane(ctx, slot); got != second {
		t.Fatalf("BelowPane = %q; want the second shell %q", got, second)
	}
	if n := strings.Fields(tmuxIn(t, h, slot, "list-panes", "-t", string(slot), "-F", "#{pane_id}")); len(n) != 3 {
		t.Fatalf("the window has panes %v; want the sidebar, the agent and one shell", n)
	}
	if alive, _ := h.Alive(ctx, first); !alive {
		t.Fatal("the replaced shell died; it should be parked")
	}
}

func TestSwitchingAgentsKeepsTheShellBelow(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	a, _ := h.Create(ctx, catPane("agent-a"))
	b, _ := h.Create(ctx, catPane("agent-b"))
	shell, _ := h.Create(ctx, catPane("shell"))
	if err := h.Show(ctx, a, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.ShowBelow(ctx, shell, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, b, slot); err != nil {
		t.Fatal(err)
	}
	if h.ShownIn(ctx, slot) != b || h.BelowPane(ctx, slot) != shell {
		t.Fatalf("after Show: slot %q below %q; want %q and %q", h.ShownIn(ctx, slot), h.BelowPane(ctx, slot), b, shell)
	}
}

func outerTerminal(t *testing.T, h *tmux.Host, slot app.Slot) func(args ...string) string {
	t.Helper()
	socket := "crew-outer-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000")
	attach := strings.Join(append([]string{"env", "-u", "TMUX"}, h.AttachCommand(slot)...), " ")
	run := func(args ...string) string {
		out, err := exec.Command("tmux", append([]string{"-L", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("outer tmux %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	run("new-session", "-d", "-s", "outer", "-x", "120", "-y", "40", attach)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "-f", "/dev/null", "kill-server").Run() })
	waitFor(t, "the client to attach", func() bool {
		return tmuxIn(t, h, slot, "list-clients") != ""
	})
	return run
}

func TestShellPopupAttachesAClientToTheKeptAliveShell(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	shell, err := h.Create(ctx, app.PaneSpec{Name: "shell", Command: []string{"sh"}})
	if err != nil {
		t.Fatal(err)
	}
	outer := outerTerminal(t, h, slot)

	if err := h.Popup(ctx, shell); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the popup client to attach", func() bool {
		return strings.Contains(tmuxIn(t, h, slot, "list-clients", "-F", "#{session_name}"), "agentws-popup")
	})
	outer("send-keys", "-t", "outer", "echo popup-$((6*7))", "Enter")
	waitFor(t, "the shell to answer in the popup", func() bool {
		return strings.Contains(outer("capture-pane", "-p", "-t", "outer"), "popup-42")
	})

	outer("send-keys", "-t", "outer", "M-t")
	waitFor(t, "the popup to close on M-t", func() bool {
		return !strings.Contains(tmuxIn(t, h, slot, "list-clients", "-F", "#{session_name}"), "agentws-popup")
	})
	if alive, _ := h.Alive(ctx, shell); !alive {
		t.Fatal("closing the popup killed the shell")
	}
	waitFor(t, "the popup session to go away", func() bool {
		return !strings.Contains(tmuxIn(t, h, slot, "list-sessions", "-F", "#{session_name}"), "agentws-popup")
	})
}

func TestPopupWithoutAClientFails(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if _, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	shell, _ := h.Create(ctx, catPane("shell"))
	if err := h.Popup(ctx, shell); err == nil {
		t.Fatal("Popup succeeded with no client attached")
	}
}

func TestNavigationKeysReachTheAppInAPaneThatIsNotNvim(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"cat"}})
	agent, _ := h.Create(ctx, app.PaneSpec{Name: "agent", Command: []string{"sh", "-c", "stty raw -echo; echo ready; exec cat -v"}})
	shell, _ := h.Create(ctx, catPane("shell"))
	if err := h.Show(ctx, agent, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.ShowBelow(ctx, shell, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.FocusSlot(ctx, slot); err != nil {
		t.Fatal(err)
	}
	outer := outerTerminal(t, h, slot)
	waitFor(t, "the agent's terminal to be raw", func() bool {
		got, _ := h.Capture(ctx, agent, 10)
		return strings.Contains(got, "ready")
	})
	for _, key := range []string{"C-h", "C-j", "C-k", "C-l"} {
		outer("send-keys", "-t", "outer", key)
	}
	waitFor(t, "the agent's terminal to receive C-h, C-k and C-l (C-j arrives as a newline)", func() bool {
		got, _ := h.Capture(ctx, agent, 10)
		return strings.Contains(got, "^H") && strings.Contains(got, "^K") && strings.Contains(got, "^L")
	})
	if active := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot), "#{pane_id}"); active != string(agent) {
		t.Fatalf("active pane = %s; the keys must not move focus away from the agent %s", active, agent)
	}
}

func TestPopupCommandRunsWithEnv(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	outer := outerTerminal(t, h, slot)
	spec := app.PaneSpec{Command: []string{"sh", "-c", `echo "dialog-$POPUP_WORD"; read line`}, Env: map[string]string{"POPUP_WORD": "ready"}}
	if err := h.PopupCommand(ctx, spec); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the command to draw in the popup", func() bool {
		return strings.Contains(outer("capture-pane", "-p", "-t", "outer"), "dialog-ready")
	})
	outer("send-keys", "-t", "outer", "Enter")
	waitFor(t, "the popup to close with the command", func() bool {
		return !strings.Contains(outer("capture-pane", "-p", "-t", "outer"), "dialog-ready")
	})
}

func TestPopupCommandWithoutAClientFails(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if _, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.PopupCommand(ctx, app.PaneSpec{Command: []string{"true"}}); err == nil {
		t.Fatal("PopupCommand succeeded with no client attached")
	}
}

func TestSetTitleShowsOnThePanesTopBorder(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	agent, _ := h.Create(ctx, catPane("agent"))
	if err := h.Show(ctx, agent, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.SetTitle(ctx, agent, "◐ claude · opus-5.5 │ api:x #7 ✓"); err != nil {
		t.Fatal(err)
	}
	if got := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(agent), "#{@agentws_title}"); got != "◐ claude · opus-5.5 │ api:x #7 ✓" {
		t.Fatalf("pane title = %q", got)
	}
	if got := tmuxIn(t, h, slot, "show-options", "-gv", "pane-border-status"); got != "top" {
		t.Fatalf("pane-border-status = %q; want titles on top borders", got)
	}
}

func TestSetTitleTurnsTitlesOnInAServerStartedBeforeThem(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	agent, _ := h.Create(ctx, catPane("agent"))
	tmuxIn(t, h, slot, "set-option", "-g", "pane-border-status", "off")
	tmuxIn(t, h, slot, "set-option", "-gu", "pane-border-format")
	if err := h.SetTitle(ctx, agent, "x"); err != nil {
		t.Fatal(err)
	}
	if got := tmuxIn(t, h, slot, "show-options", "-gv", "pane-border-status"); got != "top" {
		t.Fatalf("pane-border-status = %q; want top on the running server", got)
	}
	if got := tmuxIn(t, h, slot, "show-options", "-gv", "pane-border-format"); !strings.Contains(got, "@agentws_title") {
		t.Fatalf("pane-border-format = %q", got)
	}
}

func TestTitlesShowAHashAsText(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	agent, _ := h.Create(ctx, catPane("agent"))
	if err := h.Show(ctx, agent, slot); err != nil {
		t.Fatal(err)
	}
	outer := outerTerminal(t, h, slot)
	if err := h.SetTitle(ctx, agent, "cwd /wt/api#[default]x"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the title to be drawn as text", func() bool {
		return strings.Contains(outer("capture-pane", "-p", "-t", "outer"), "cwd /wt/api#[default]x")
	})
}

func TestDetachLeavesTheLayoutRunning(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	outerTerminal(t, h, slot)
	if err := h.Detach(ctx, slot); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the client to detach", func() bool {
		return tmuxIn(t, h, slot, "list-clients") == ""
	})
	if !h.ClientOpen(ctx, slot) {
		t.Fatal("detaching closed the layout")
	}
}

func TestPopupCommandRunsInTheGivenDir(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, _ := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	outer := outerTerminal(t, h, slot)
	dir := t.TempDir()
	if err := h.PopupCommand(ctx, app.PaneSpec{Command: []string{"sh", "-c", "echo in-$(basename $PWD); read line"}, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the popup to start in dir", func() bool {
		return strings.Contains(outer("capture-pane", "-p", "-t", "outer"), "in-"+filepath.Base(dir))
	})
	outer("send-keys", "-t", "outer", "Enter")
}
