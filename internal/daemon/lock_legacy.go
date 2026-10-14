package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func legacyLockPath(home string) string { return filepath.Join(home, "agentws.lock") }
func legacyPIDPath(home string) string  { return filepath.Join(home, "agentws.pid") }

func holdLegacyLock(home string, current *os.File) error {
	old, err := os.OpenFile(legacyLockPath(home), os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = old.Close() }()
	err = syscall.Flock(int(old.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return syscall.Flock(int(old.Fd()), syscall.LOCK_UN)
	}
	_ = current.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) {
		if pid, ok := ReadPID(home); ok {
			return fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, pid)
		}
		return ErrAlreadyRunning
	}
	return err
}

func readPIDFile(home string) ([]byte, error) {
	b, err := os.ReadFile(PIDPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return os.ReadFile(legacyPIDPath(home))
	}
	return b, err
}
