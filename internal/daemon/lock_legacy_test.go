package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "crew-ll")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

func TestTheLockAndPidFilesAreNamedAfterCrew(t *testing.T) {
	if got := LockPath("/h"); got != "/h/crew.lock" {
		t.Errorf("LockPath = %s", got)
	}
	if got := PIDPath("/h"); got != "/h/crew.pid" {
		t.Errorf("PIDPath = %s", got)
	}
}

func TestADaemonDoesNotStartWhileAnOldDaemonHoldsItsLock(t *testing.T) {
	home := shortHome(t)
	old, err := os.OpenFile(filepath.Join(home, "agentws.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Close() })
	if err := syscall.Flock(int(old.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(home); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Acquire = %v, want ErrAlreadyRunning", err)
	}
	if _, err := os.Stat(PIDPath(home)); err == nil {
		t.Fatal("a refused daemon wrote a pid file")
	}
}

func TestReadPIDFindsTheOldPidFileWhileTheNewOneIsMissing(t *testing.T) {
	home := shortHome(t)
	if err := os.WriteFile(filepath.Join(home, "agentws.pid"), []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pid, ok := ReadPID(home); !ok || pid != 4242 {
		t.Fatalf("ReadPID = %d, %v", pid, ok)
	}
}
