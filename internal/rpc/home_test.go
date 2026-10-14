package rpc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
)

func homeWith(t *testing.T, dirs ...string) string {
	t.Helper()
	user := t.TempDir()
	t.Setenv("HOME", user)
	t.Setenv("CREW_HOME", "")
	t.Setenv("AGENTWS_HOME", "")
	for _, d := range dirs {
		if err := os.Mkdir(filepath.Join(user, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return user
}

func TestHomeIsTheCrewDirOnAFreshInstall(t *testing.T) {
	user := homeWith(t)
	if got, err := rpc.Home(); err != nil || got != filepath.Join(user, ".crew") {
		t.Fatalf("Home() = %q, %v", got, err)
	}
}

func TestHomeStaysOnAnAgentwsDirThatIsNotMovedYet(t *testing.T) {
	user := homeWith(t, ".agentws")
	if got, err := rpc.Home(); err != nil || got != filepath.Join(user, ".agentws") {
		t.Fatalf("Home() = %q, %v", got, err)
	}
}

func TestHomeIsTheCrewDirOnceItExists(t *testing.T) {
	user := homeWith(t, ".agentws", ".crew")
	if got, err := rpc.Home(); err != nil || got != filepath.Join(user, ".crew") {
		t.Fatalf("Home() = %q, %v", got, err)
	}
}
