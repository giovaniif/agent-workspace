package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var ErrAlreadyRunning = errors.New("already running")

func LockPath(home string) string { return filepath.Join(home, "crew.lock") }
func PIDPath(home string) string  { return filepath.Join(home, "crew.pid") }

type Lock struct {
	file *os.File
	home string
}

func Acquire(home string) (*Lock, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(LockPath(home), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			if pid, ok := ReadPID(home); ok {
				return nil, fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, pid)
			}
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	if err := holdLegacyLock(home, f); err != nil {
		return nil, err
	}
	if err := os.WriteFile(PIDPath(home), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{file: f, home: home}, nil
}

func (l *Lock) Release() error {
	err := os.Remove(PIDPath(l.home))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	unlock := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return errors.Join(err, unlock, l.file.Close())
}

func ReadPID(home string) (int, bool) {
	b, err := readPIDFile(home)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}
