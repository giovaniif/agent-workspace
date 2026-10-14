package procs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.ProcessTable = Table{}

const (
	DefaultGrace = 3 * time.Second
	pollEvery    = 25 * time.Millisecond
	killWait     = time.Second
)

type Table struct {
	Grace time.Duration
}

func (Table) Listeners(ctx context.Context) ([]domain.Listener, error) {
	sockets, err := run(ctx, "netstat", "-anv", "-p", "tcp")
	if err != nil {
		return nil, err
	}
	listeners := parseNetstat(sockets)
	if len(listeners) == 0 {
		return nil, nil
	}
	found, err := run(ctx, "lsof", "-nP", "-a", "-d", "cwd", "-p", pidList(listeners), "+c", "0", "-Fpgcn")
	if err != nil {
		return nil, err
	}
	return merge(listeners, parseDetails(found)), nil
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

func (t Table) Terminate(ctx context.Context, pgid int) error {
	if pgid <= 1 || pgid == syscall.Getpgrp() {
		return fmt.Errorf("refusing to signal process group %d", pgid)
	}
	if err := signalGroup(pgid, syscall.SIGTERM); err != nil {
		return err
	}
	grace := t.Grace
	if grace == 0 {
		grace = DefaultGrace
	}
	if waitGone(ctx, pgid, grace) {
		return nil
	}
	if err := signalGroup(pgid, syscall.SIGKILL); err != nil {
		return err
	}
	if !waitGone(ctx, pgid, killWait) {
		return fmt.Errorf("process group %d survived SIGKILL", pgid)
	}
	return nil
}

func signalGroup(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process group %d: %w", pgid, err)
	}
	return nil
}

func waitGone(ctx context.Context, pgid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(pollEvery)
	}
}
