//go:build integration

package tmux_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/app"
)

func TestNavigationKeysPassThroughNvimAndLeaveItAtTheEdge(t *testing.T) {
	plugin, err := filepath.Abs(filepath.Join("..", "..", "..", "nvim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plugin, "lua", "agentws", "init.lua")); err != nil {
		t.Fatalf("the nvim plugin is missing: %v", err)
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "agentws-nav")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	initLua := filepath.Join(dir, "init.lua")
	if err := os.WriteFile(initLua, []byte("vim.opt.rtp:prepend('"+plugin+"')\nrequire('agentws').setup({})\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "n.sock")

	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"cat"}})
	if err != nil {
		t.Fatal(err)
	}
	editor, err := h.Create(ctx, app.PaneSpec{Name: "nvim", Command: []string{"nvim", "--clean", "-u", initLua, "--listen", sock, "-c", "vsplit", "-c", "wincmd l"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, editor, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.FocusSlot(ctx, slot); err != nil {
		t.Fatal(err)
	}
	outer := outerTerminal(t, h, slot)
	sidebar := tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot)+".0", "#{pane_id}")
	activePane := func() string {
		return tmuxIn(t, h, slot, "display-message", "-p", "-t", string(slot), "#{pane_id}")
	}
	nvimWindow := func() string {
		out, err := exec.Command("nvim", "--server", sock, "--remote-expr", "winnr()").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	waitFor(t, "nvim to start on its right split", func() bool { return nvimWindow() == "2" })
	waitFor(t, "tmux to see nvim as the pane's command", func() bool {
		return tmuxIn(t, h, slot, "display-message", "-p", "-t", string(editor), "#{pane_current_command}") == "nvim"
	})

	steps := []struct {
		key        string
		wantWindow string
		wantPane   string
	}{
		{"C-h", "1", string(editor)},
		{"C-l", "2", string(editor)},
		{"C-l", "2", string(editor)},
		{"C-h", "1", string(editor)},
		{"C-h", "1", sidebar},
	}
	tmuxIn(t, h, slot, "select-pane", "-t", string(editor))
	for i, s := range steps {
		outer("send-keys", "-t", "outer", s.key)
		waitFor(t, "step "+string(rune('1'+i))+" ("+s.key+") to reach window "+s.wantWindow+" of pane "+s.wantPane, func() bool {
			return activePane() == s.wantPane && nvimWindow() == s.wantWindow
		})
	}
}
