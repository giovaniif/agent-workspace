//go:build integration

package procs_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/procs"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.ProcessTable = procs.Table{}

const helperEnv = "CREW_PROCS_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fallthrough
	default:
		serve()
	}
}

func serve() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	child := exec.Command("sleep", "300")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Printf("%d %d\n", port, child.Process.Pid)
	time.Sleep(time.Hour)
}

type server struct {
	cmd   *exec.Cmd
	port  int
	child int
	dir   string
}

func startServer(t *testing.T, mode string) server {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), helperEnv+"="+mode)
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
		t.Fatalf("server did not start: %v", err)
	}
	fields := strings.Fields(line)
	port, _ := strconv.Atoi(fields[0])
	child, _ := strconv.Atoi(fields[1])
	return server{cmd: cmd, port: port, child: child, dir: dir}
}

func listener(t *testing.T, port int) (domain.Listener, bool) {
	t.Helper()
	ls, err := procs.Table{}.Listeners(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if l.Port == port {
			return l, true
		}
	}
	return domain.Listener{}, false
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestPortsListenerReportsPidGroupCommandAndCwd(t *testing.T) {
	s := startServer(t, "serve")
	l, ok := listener(t, s.port)
	if !ok {
		t.Fatalf("port %d not listed", s.port)
	}
	if l.PID != s.cmd.Process.Pid || l.PGID != s.cmd.Process.Pid || l.Cwd != s.dir {
		t.Errorf("got %+v, want pid and group %d, cwd %s", l, s.cmd.Process.Pid, s.dir)
	}
	if l.Command == "" {
		t.Error("no command")
	}
}

func TestPortsTerminateKillsTheGroupAndFreesThePort(t *testing.T) {
	s := startServer(t, "serve")
	if !alive(s.child) {
		t.Fatal("child not running")
	}
	if _, ok := listener(t, s.port); !ok {
		t.Fatalf("port %d not listed before the kill", s.port)
	}
	if err := (procs.Table{}).Terminate(context.Background(), s.cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if _, ok := listener(t, s.port); ok {
		t.Errorf("port %d still listed", s.port)
	}
	if alive(s.child) {
		t.Error("child survived")
	}
}

func TestPortsTerminateEscalatesWhenTermIsIgnored(t *testing.T) {
	s := startServer(t, "ignore-term")
	if _, ok := listener(t, s.port); !ok {
		t.Fatalf("port %d not listed before the kill", s.port)
	}
	table := procs.Table{Grace: 300 * time.Millisecond}
	start := time.Now()
	if err := table.Terminate(context.Background(), s.cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 250*time.Millisecond {
		t.Errorf("gave up on TERM after %v", took)
	}
	if _, ok := listener(t, s.port); ok {
		t.Errorf("port %d still listed", s.port)
	}
}

func TestPortsTerminateRefusesUnsafeGroups(t *testing.T) {
	for _, pgid := range []int{-1, 0, 1, syscall.Getpgrp()} {
		if err := (procs.Table{}).Terminate(context.Background(), pgid); err == nil {
			t.Errorf("Terminate(%d) succeeded", pgid)
		}
	}
}

func TestPortsTerminateGoneGroupIsNotAnError(t *testing.T) {
	s := startServer(t, "serve")
	table := procs.Table{}
	if err := table.Terminate(context.Background(), s.cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := table.Terminate(context.Background(), s.cmd.Process.Pid); err != nil {
		t.Errorf("got %v", err)
	}
}

func BenchmarkPortsRefresh(b *testing.B) {
	dir, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), helperEnv+"=serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		b.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }()
	if _, err := bufio.NewReader(out).ReadString('\n'); err != nil {
		b.Fatal(err)
	}
	table := procs.Table{}
	b.ResetTimer()
	start := time.Now()
	for range b.N {
		if _, err := table.Listeners(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
	perOp := time.Since(start) / time.Duration(b.N)
	b.ReportMetric(float64(perOp.Microseconds())/1000, "ms/refresh")
	if perOp > 50*time.Millisecond {
		b.Fatalf("a refresh took %v, budget 50ms", perOp)
	}
}
