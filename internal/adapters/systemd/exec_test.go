package systemd_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/systemd"
)

func fakeSystemctlNeedingTheUserBus(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n[ -n \"$XDG_RUNTIME_DIR\" ] || { echo 'Failed to connect to bus: No medium found' >&2; exit 1; }\necho \"$XDG_RUNTIME_DIR $DBUS_SESSION_BUS_ADDRESS\" > \"$LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	log := filepath.Join(bin, "env.log")
	t.Setenv("LOG", log)
	return log
}

func TestSystemctlOverANonInteractiveSSHFindsTheLingeringUserBus(t *testing.T) {
	log := fakeSystemctlNeedingTheUserBus(t)
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := systemd.ExecIn(root)(context.Background(), "systemctl", "--user", "daemon-reload"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if want := dir + " unix:path=" + dir + "/bus\n"; string(got) != want {
		t.Fatalf("systemctl saw %q, want %q", got, want)
	}
}

func TestSystemctlWithoutAUserRuntimeDirAsksForLinger(t *testing.T) {
	fakeSystemctlNeedingTheUserBus(t)
	err := systemd.ExecIn(t.TempDir())(context.Background(), "systemctl", "--user", "daemon-reload")
	if err == nil || !strings.Contains(err.Error(), "loginctl enable-linger") {
		t.Fatalf("error %v, want a hint to enable linger", err)
	}
}
