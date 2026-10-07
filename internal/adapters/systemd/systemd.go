package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Runner func(ctx context.Context, name string, args ...string) error

var Exec = ExecIn("/run/user")

func ExecIn(runtimeRoot string) Runner {
	return func(ctx context.Context, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		hint := ""
		if os.Getenv("XDG_RUNTIME_DIR") == "" {
			dir := filepath.Join(runtimeRoot, strconv.Itoa(os.Getuid()))
			info, err := os.Stat(dir)
			switch {
			case err == nil && info.IsDir():
				cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+dir, "DBUS_SESSION_BUS_ADDRESS=unix:path="+dir+"/bus")
			case err != nil && !errors.Is(err, os.ErrNotExist):
				hint = "; " + err.Error()
			default:
				hint = "; no user runtime dir " + dir + ", so systemd --user needs linger: sudo loginctl enable-linger $USER"
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %v: %w: %s%s", name, args, err, bytes.TrimSpace(out), hint)
		}
		return nil
	}
}

type Unit struct {
	Dir     string
	Name    string
	Program []string
	Env     map[string]string
	Log     string
}

type Result struct {
	Changed bool
	Path    string
	Backup  string
}

func (u Unit) file() string { return u.Name + ".service" }
func (u Unit) path() string { return filepath.Join(u.Dir, u.file()) }

func Install(ctx context.Context, u Unit, run Runner) (Result, error) {
	res := Result{Path: u.path()}
	want, err := render(u)
	if err != nil {
		return res, err
	}
	old, err := os.ReadFile(res.Path)
	switch {
	case err == nil && bytes.Equal(old, want):
		if run(ctx, "systemctl", "--user", "is-active", u.file()) == nil {
			return res, nil
		}
		return res, start(ctx, u, run)
	case err == nil:
		res.Backup = res.Path + ".bak"
		if err := os.WriteFile(res.Backup, old, 0o644); err != nil {
			return res, err
		}
	case !errors.Is(err, os.ErrNotExist):
		return res, err
	}
	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(res.Path, want, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	if err := run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return res, err
	}
	return res, start(ctx, u, run)
}

func start(ctx context.Context, u Unit, run Runner) error {
	if err := run(ctx, "systemctl", "--user", "enable", u.file()); err != nil {
		return err
	}
	return run(ctx, "systemctl", "--user", "restart", u.file())
}

func Remove(ctx context.Context, u Unit, run Runner) (bool, error) {
	if _, err := os.Stat(u.path()); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := run(ctx, "systemctl", "--user", "disable", "--now", u.file()); err != nil {
		return false, err
	}
	if err := os.Remove(u.path()); err != nil {
		return false, err
	}
	return true, run(ctx, "systemctl", "--user", "daemon-reload")
}

var logEscaper = strings.NewReplacer(`\`, `\\`, `%`, `%%`)

var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)

func quote(s string) string { return `"` + escaper.Replace(s) + `"` }

func render(u Unit) ([]byte, error) {
	values := append([]string{u.Name, u.Log}, u.Program...)
	for k, v := range u.Env {
		values = append(values, k, v)
	}
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("a unit value has a line break or NUL: %q", v)
		}
	}
	var b bytes.Buffer
	b.WriteString("[Unit]\nDescription=" + u.Name + "\nAfter=network-online.target\n\n[Service]\n")
	quoted := make([]string, len(u.Program))
	for i, arg := range u.Program {
		quoted[i] = quote(arg)
	}
	b.WriteString("ExecStart=" + strings.Join(quoted, " ") + "\n")
	names := make([]string, 0, len(u.Env))
	for k := range u.Env {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		b.WriteString("Environment=" + quote(k+"="+u.Env[k]) + "\n")
	}
	b.WriteString("Restart=always\nRestartSec=2\n")
	if u.Log != "" {
		b.WriteString("StandardOutput=append:" + logEscaper.Replace(u.Log) + "\nStandardError=append:" + logEscaper.Replace(u.Log) + "\n")
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.Bytes(), nil
}
