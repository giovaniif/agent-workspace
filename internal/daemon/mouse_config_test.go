package daemon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/daemon"
)

func TestMouseSettingTurnsTheTmuxMouseOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if off, err := daemon.LoadNoMouse(path); err != nil || off {
		t.Fatalf("missing file: off %v, %v; want the mouse on", off, err)
	}
	if err := os.WriteFile(path, []byte("[ui]\nmouse = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if off, err := daemon.LoadNoMouse(path); err != nil || !off {
		t.Fatalf("mouse = false: off %v, %v; want off", off, err)
	}
	if err := os.WriteFile(path, []byte("[ui]\nmouse = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if off, _ := daemon.LoadNoMouse(path); off {
		t.Fatal("mouse = true turned it off")
	}
}
