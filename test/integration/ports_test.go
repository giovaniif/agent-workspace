//go:build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const serverEnv = "AGENTWS_PORTS_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(serverEnv) == "" {
		os.Exit(m.Run())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	child := exec.Command("sleep", "300")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	fmt.Printf("%d %d\n", ln.Addr().(*net.TCPAddr).Port, child.Process.Pid)
	time.Sleep(time.Hour)
}

type devServer struct {
	pid, port, child int
}

func startDevServer(t *testing.T, dir string) devServer {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), serverEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-reaped
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dev server did not start: %v", err)
	}
	f := strings.Fields(line)
	port, _ := strconv.Atoi(f[0])
	child, _ := strconv.Atoi(f[1])
	return devServer{pid: cmd.Process.Pid, port: port, child: child}
}

func portsOn(st rpc.State, path string) []domain.Port {
	w, _ := find(st, path)
	return w.Ports
}

const refreshBudget = 6 * time.Second

func TestPortsServerInAWorktreeShowsUpMappedToIt(t *testing.T) {
	e := start(t, 100*time.Millisecond, time.Hour)
	api := filepath.Join(e.root, "api")
	feat := filepath.Join(e.root, "api-ports")
	git(t, api, "worktree", "add", "-q", "-b", "ports", feat)
	e.waitFor(t, 5*time.Second, "worktree listed", func(st rpc.State) bool { _, ok := find(st, feat); return ok })

	srv := startDevServer(t, filepath.Join(feat))
	st := e.waitFor(t, refreshBudget, "port mapped to the worktree", func(st rpc.State) bool { return len(portsOn(st, feat)) > 0 })

	got := portsOn(st, feat)[0]
	if got.Port != srv.port || got.PID != srv.pid || got.PGID != srv.pid {
		t.Errorf("port = %+v, want port %d pid and group %d", got, srv.port, srv.pid)
	}
	if other := portsOn(st, filepath.Join(e.root, "api-old")); len(other) != 0 {
		t.Errorf("api-old has ports %+v", other)
	}
}

func TestPortsKillEndsServerAndChildrenAndTheNextRefreshDropsThePort(t *testing.T) {
	e := start(t, 100*time.Millisecond, time.Hour)
	api := filepath.Join(e.root, "api")
	feat := filepath.Join(e.root, "api-ports")
	git(t, api, "worktree", "add", "-q", "-b", "ports", feat)
	e.waitFor(t, 5*time.Second, "worktree listed", func(st rpc.State) bool { _, ok := find(st, feat); return ok })
	srv := startDevServer(t, feat)
	st := e.waitFor(t, refreshBudget, "port mapped", func(st rpc.State) bool { return len(portsOn(st, feat)) > 0 })

	killed, err := e.client.KillPorts(context.Background(), []int{portsOn(st, feat)[0].PGID})
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != srv.pid {
		t.Errorf("killed %v, want [%d]", killed, srv.pid)
	}
	if syscall.Kill(srv.child, 0) == nil {
		t.Error("the server's child survived")
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", srv.port), time.Second); err == nil {
		_ = c.Close()
		t.Error("the port still accepts connections")
	}
	e.waitFor(t, refreshBudget, "port gone from the worktree", func(st rpc.State) bool { return len(portsOn(st, feat)) == 0 })
}

func TestPortsKillRefusesAGroupThatServesNoPort(t *testing.T) {
	e := start(t, time.Hour, time.Hour)
	if _, err := e.client.KillPorts(context.Background(), []int{syscall.Getpgrp()}); err == nil {
		t.Fatal("killed the test's own group")
	}
}
