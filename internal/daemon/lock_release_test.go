package daemon

import (
	"os"
	"syscall"
	"testing"
)

func TestReleaseFreesTheLockEvenWhileAForkedChildStillHoldsItsFile(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "crew-l")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	lock, err := Acquire(home)
	if err != nil {
		t.Fatal(err)
	}
	child, err := syscall.Dup(int(lock.file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(child) })
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := Acquire(home)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = again.Release()
}
