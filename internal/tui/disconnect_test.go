package tui_test

import (
	"net"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

var ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

func connectedModel(c *fakeCaller) tui.Model {
	st := fixture(2, 1)
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	return update(m, tui.StateMsg(st))
}

func quitsOn(t *testing.T, m tui.Model, k tea.KeyPressMsg) bool {
	t.Helper()
	_, cmd := m.Update(k)
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitWhileDisconnectedQuitsWithoutCallingTheDaemon(t *testing.T) {
	overlays := []struct {
		name string
		keys []string
		q    bool
	}{
		{"the list", nil, true},
		{"help", []string{"?"}, true},
		{"the end prompt", []string{"x"}, true},
		{"the model picker", []string{"M"}, true},
		{"the resume picker", []string{"u"}, true},
		{"the rename field", []string{"R"}, false},
		{"the launcher", []string{"L"}, false},
		{"the new-session dialog", []string{"n"}, false},
	}
	for _, o := range overlays {
		t.Run(o.name, func(t *testing.T) {
			c := &fakeCaller{}
			m := update(connectedModel(c), tui.DisconnectedMsg{})
			m = press(m, o.keys...)
			c.calls = nil
			keys := []tea.KeyPressMsg{ctrlC}
			if o.q {
				keys = append(keys, key("q"))
			}
			for _, k := range keys {
				if !quitsOn(t, m, k) {
					t.Errorf("%s in %s does not quit while the daemon is disconnected", k, o.name)
				}
			}
			if len(c.calls) != 0 {
				t.Errorf("quitting while disconnected called the daemon: %v", c.methods())
			}
		})
	}
}

func TestQuitWhenTheConnectionIsAlreadyGone(t *testing.T) {
	for _, err := range []error{
		rpc.ErrClosed,
		&net.OpError{Op: "write", Net: "unix", Err: syscall.EPIPE},
		&net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED},
	} {
		c := &fakeCaller{err: err}
		m := connectedModel(c)
		for _, k := range []tea.KeyPressMsg{key("q"), ctrlC} {
			if !quitsOn(t, m, k) {
				t.Errorf("%s does not quit when the detach fails with %v", k, err)
			}
		}
	}
}

func TestQuitWhenTheDaemonIsAnotherBuild(t *testing.T) {
	c := &fakeCaller{err: rpc.Mismatch("v2", "v1", 2, 1)}
	m := connectedModel(c)
	for _, k := range []tea.KeyPressMsg{key("q"), ctrlC} {
		if !quitsOn(t, m, k) {
			t.Errorf("%s does not quit when the daemon answers version_mismatch", k)
		}
	}
}

func TestQQuitsTheSetupWalkthroughWhileDisconnected(t *testing.T) {
	m, _ := setupModel(t, freshMachine())
	m = update(m, tui.DisconnectedMsg{})
	if !quitsOn(t, m, key("q")) {
		t.Fatal("q in the setup walkthrough does not quit while the daemon is disconnected")
	}
}

func TestQKeepsTheSidebarOnADaemonFailureThatIsNotALostConnection(t *testing.T) {
	c := &fakeCaller{err: &rpc.Error{Code: rpc.CodeFailed, Message: "tmux: server exited"}}
	if quitsOn(t, connectedModel(c), key("q")) {
		t.Fatal("q quit on a tmux failure while the daemon is connected")
	}
}
