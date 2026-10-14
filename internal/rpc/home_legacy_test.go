package rpc_test

import (
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
)

func clearLegacyHome(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTWS_HOME", "")
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
