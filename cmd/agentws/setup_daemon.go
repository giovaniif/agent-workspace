package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/giovaniif/agent-workspace/internal/adapters/launchd"
	"github.com/giovaniif/agent-workspace/internal/adapters/loginshell"
	"github.com/giovaniif/agent-workspace/internal/adapters/systemd"
)

const setupDaemonUsage = "usage: agentws setup daemon [--remove | --check]"

const (
	daemonUnitName   = "agentws-daemon"
	daemonAgentLabel = "dev.agentws.daemon"
)

type daemonCheck struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	Linger    bool `json:"linger"`
}

func runSetupDaemon(args []string, stdout, stderr io.Writer, env func(string) string, self, goos string) int {
	fs := flag.NewFlagSet("setup daemon", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remove := fs.Bool("remove", false, "")
	check := fs.Bool("check", false, "")
	err := fs.Parse(args)
	switch {
	case err != nil:
	case fs.NArg() != 0:
		err = errors.New("takes no arguments, only flags")
	case *remove && *check:
		err = errors.New("--remove and --check go alone")
	case goos != "linux" && goos != "darwin":
		err = fmt.Errorf("%s is not supported, only linux and macOS", goos)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: %v\n%s\n", err, setupDaemonUsage)
		return 2
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: %v\n", err)
		return 1
	}
	ctx := context.Background()
	svc := daemonService(env, self, userHome, os.Getuid(), goos)
	switch {
	case *check:
		return printDaemonCheck(ctx, svc, env, stdout, stderr)
	case *remove:
		removed, err := removeServe(ctx, svc)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "agentws setup daemon: %v\n", err)
			return 1
		case removed:
			fmt.Fprintf(stdout, "stopped and removed %s\n", svc.path())
		default:
			fmt.Fprintf(stdout, "nothing to remove at %s\n", svc.path())
		}
		return 0
	}
	loginPath, err := loginshell.Path(ctx, env("SHELL"))
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: reading the login shell's PATH: %v\n", err)
		return 1
	}
	if svc.launchd {
		svc.agent.Env["PATH"] = loginPath
	} else {
		svc.unit.Env["PATH"] = loginPath
	}
	path, backup, changed, err := installServe(ctx, svc)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: %v\n", err)
		return 1
	}
	if changed {
		fmt.Fprintf(stdout, "wrote and started %s; the daemon now runs at login and logs to %s\n", path, serveLog(svc))
		if backup != "" {
			fmt.Fprintf(stdout, "previous file saved as %s\n", backup)
		}
	} else {
		fmt.Fprintf(stdout, "already set up in %s\n", path)
	}
	if svc.launchd {
		return 0
	}
	if on, err := lingerOn(ctx, env); err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: could not check linger: %v\n", err)
	} else if !on {
		fmt.Fprintf(stdout, "linger is off for %s: the daemon and every session stop at logout. Fix it with:\n  sudo loginctl enable-linger %s\n", env("USER"), env("USER"))
	}
	return 0
}

func daemonService(env func(string) string, self, userHome string, uid int, goos string) serveService {
	home := env("AGENTWS_HOME")
	if home == "" {
		home = filepath.Join(userHome, ".agentws")
	}
	program := []string{self, "daemon"}
	svcEnv := map[string]string{"AGENTWS_HOME": home}
	log := filepath.Join(home, "daemon-service.log")
	if goos == "darwin" {
		return serveService{launchd: true, agent: launchd.Agent{
			Dir:     filepath.Join(userHome, "Library", "LaunchAgents"),
			Label:   daemonAgentLabel,
			Program: program,
			Env:     svcEnv,
			Log:     log,
			UID:     uid,
		}}
	}
	config := env("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(userHome, ".config")
	}
	return serveService{unit: systemd.Unit{
		Dir:     filepath.Join(config, "systemd", "user"),
		Name:    daemonUnitName,
		Program: program,
		Env:     svcEnv,
		Log:     log,
	}}
}

func printDaemonCheck(ctx context.Context, svc serveService, env func(string) string, stdout, stderr io.Writer) int {
	var c daemonCheck
	switch _, err := os.Stat(svc.path()); {
	case err == nil:
		c.Installed = true
	case !errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "agentws setup daemon: %v\n", err)
		return 1
	}
	if svc.launchd {
		c.Running = launchd.Exec(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/%s", svc.agent.UID, svc.agent.Label)) == nil
		c.Linger = true
	} else {
		c.Running = systemd.Exec(ctx, "systemctl", "--user", "is-active", svc.unit.Name+".service") == nil
		on, err := lingerOn(ctx, env)
		if err != nil {
			fmt.Fprintf(stderr, "agentws setup daemon: could not check linger: %v\n", err)
			return 1
		}
		c.Linger = on
	}
	if err := json.NewEncoder(stdout).Encode(c); err != nil {
		fmt.Fprintf(stderr, "agentws setup daemon: %v\n", err)
		return 1
	}
	return 0
}

func lingerOn(ctx context.Context, env func(string) string) (bool, error) {
	out, err := exec.CommandContext(ctx, "loginctl", "show-user", env("USER"), "-p", "Linger").Output()
	if err != nil {
		return false, fmt.Errorf("loginctl: %w", err)
	}
	return bytes.Equal(bytes.TrimSpace(out), []byte("Linger=yes")), nil
}
