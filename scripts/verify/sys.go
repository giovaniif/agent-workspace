package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type cmd struct {
	dir   string
	env   []string
	name  string
	args  []string
	stdin string
}

type sys interface {
	exec(c cmd) (string, string, int)
	look(name string) (string, bool)
	getenv(k string) string
	start(c cmd, log string) (int, error)
	kill(pid int)
	portFree(p int) bool
	freePort() int
	load() (float64, int)
	request(method, url, body string) (int, string, error)
}

type realSys struct{}

func (realSys) build(c cmd) *exec.Cmd {
	x := exec.Command(c.name, c.args...)
	x.Dir = c.dir
	if c.env != nil {
		x.Env = c.env
	}
	if c.stdin != "" {
		x.Stdin = strings.NewReader(c.stdin)
	}
	return x
}

func (s realSys) exec(c cmd) (string, string, int) {
	x := s.build(c)
	var out, errb bytes.Buffer
	x.Stdout, x.Stderr = &out, &errb
	err := x.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errb.String(), 0
	case errors.As(err, &ee):
		return out.String(), errb.String(), ee.ExitCode()
	}
	return out.String(), err.Error(), 127
}

func (realSys) look(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

func (realSys) getenv(k string) string { return os.Getenv(k) }

func (s realSys) start(c cmd, log string) (int, error) {
	f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	x := s.build(c)
	x.Stdout, x.Stderr = f, f
	x.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := x.Start(); err != nil {
		return 0, err
	}
	go func() { _ = x.Wait() }()
	return x.Process.Pid, nil
}

func (realSys) kill(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	for range 20 {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func (realSys) portFree(p int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func (realSys) freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func (realSys) load() (float64, int) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, runtime.NumCPU()
	}
	f := strings.Fields(string(b))
	v, _ := strconv.ParseFloat(f[0], 64)
	return v, runtime.NumCPU()
}

func (realSys) request(method, url, body string) (int, string, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), err
}
