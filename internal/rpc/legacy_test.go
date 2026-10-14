package rpc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheSocketIsNamedAfterCrewAndAnOldDaemonSocketIsUsedOnlyWhileTheNewOneIsMissing(t *testing.T) {
	home := t.TempDir()
	if got := rpc.SocketPath(home); got != filepath.Join(home, "crew.sock") {
		t.Fatalf("no socket yet: %s", got)
	}
	touch(t, filepath.Join(home, "agentws.sock"))
	if got := rpc.SocketPath(home); got != filepath.Join(home, "agentws.sock") {
		t.Fatalf("only the old socket exists: %s", got)
	}
	touch(t, filepath.Join(home, "crew.sock"))
	if got := rpc.SocketPath(home); got != filepath.Join(home, "crew.sock") {
		t.Fatalf("both exist: %s", got)
	}
}

func TestADaemonAlwaysListensOnTheCrewSocket(t *testing.T) {
	home := t.TempDir()
	touch(t, filepath.Join(home, "agentws.sock"))
	if got := rpc.ListenPath(home); got != filepath.Join(home, "crew.sock") {
		t.Fatalf("listen path: %s", got)
	}
}
