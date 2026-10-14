package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/giovaniif/crew/internal/adapters/loginshell"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func runDaemon(args []string, stdout, stderr io.Writer) int {
	home, err := rpc.Home()
	if err != nil {
		fmt.Fprintf(stderr, "crew daemon: %v\n", err)
		return 1
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		err = foreground(home)
	case "start":
		err = start(home, stdout)
	case "status":
		err = status(home, stdout)
	case "stop":
		err = stop(home, stdout)
	default:
		fmt.Fprintf(stderr, "crew daemon: unknown subcommand %q\nusage: crew daemon [start|status|stop]\n", sub)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "crew daemon: %v\n", err)
		return 1
	}
	return 0
}

func foreground(home string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	adoptLoginPath(ctx)
	return daemon.Run(ctx, home)
}

func adoptLoginPath(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, loginPathWithin)
	defer cancel()
	login, err := loginshell.Path(ctx, os.Getenv("SHELL"))
	if err != nil {
		log.Printf("login shell PATH ignored: %v", err)
		return
	}
	if err := os.Setenv("PATH", domain.MergeLoginPath(os.Getenv("PATH"), login)); err != nil {
		log.Printf("login shell PATH ignored: %v", err)
	}
}

const loginPathWithin = time.Second

func spawn(home string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(home, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	cmd := exec.Command(self, "daemon")
	cmd.Env = append(os.Environ(), domain.EnvName("HOME")+"="+home)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func start(home string, stdout io.Writer) error {
	if c, err := rpc.Dial(rpc.SocketPath(home)); err == nil {
		defer func() { _ = c.Close() }()
		st, err := c.Status(context.Background())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "crew daemon already running (pid %d)\n", st.PID)
		return nil
	}
	st, err := connectStatus(home)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "crew daemon started (pid %d)\n", st.PID)
	return nil
}

func connectStatus(home string) (rpc.Status, error) {
	ctx := context.Background()
	c, err := rpc.Connect(ctx, rpc.SocketPath(home), func() error { return spawn(home) })
	if err != nil {
		return rpc.Status{}, err
	}
	defer func() { _ = c.Close() }()
	return c.Status(ctx)
}

func status(home string, stdout io.Writer) error {
	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		return errNotRunning
	}
	defer func() { _ = c.Close() }()
	st, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "pid %d\nuptime %s\nsessions %d\nworktrees %d\n",
		st.PID, time.Since(st.StartedAt).Round(time.Second), st.Sessions, st.Worktrees)
	return nil
}

var errNotRunning = errors.New("not running")

func stop(home string, stdout io.Writer) error {
	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		return errNotRunning
	}
	st, err := c.Status(context.Background())
	_ = c.Close()
	if err != nil {
		return err
	}
	if err := syscall.Kill(st.PID, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(rpc.StartTimeout)
	for {
		if _, ok := daemon.ReadPID(home); !ok {
			fmt.Fprintf(stdout, "crew daemon stopped (pid %d)\n", st.PID)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pid %d did not exit within %v", st.PID, rpc.StartTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
