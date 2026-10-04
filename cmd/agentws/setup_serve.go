package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/giovaniif/agent-workspace/internal/adapters/launchd"
	"github.com/giovaniif/agent-workspace/internal/adapters/systemd"
)

const setupServeUsage = "usage: agentws setup serve [--remove] [--addr host:port] [--cert file --key file | --self-signed]"

const (
	serveUnitName   = "agentws-serve"
	serveAgentLabel = "dev.agentws.serve"
)

type serveService struct {
	launchd bool
	unit    systemd.Unit
	agent   launchd.Agent
}

func (s serveService) path() string {
	if s.launchd {
		return filepath.Join(s.agent.Dir, s.agent.Label+".plist")
	}
	return filepath.Join(s.unit.Dir, s.unit.Name+".service")
}

func runSetupServe(args []string, stdout, stderr io.Writer, env func(string) string, self, goos string) int {
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup serve: %v\n", err)
		return 1
	}
	svc, remove, err := setupServeService(args, env, self, userHome, os.Getuid(), goos)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup serve: %v\n%s\n", err, setupServeUsage)
		return 2
	}
	ctx := context.Background()
	if remove {
		removed, err := removeServe(ctx, svc)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "agentws setup serve: %v\n", err)
			return 1
		case removed:
			fmt.Fprintf(stdout, "stopped and removed %s\n", svc.path())
		default:
			fmt.Fprintf(stdout, "nothing to remove at %s\n", svc.path())
		}
		return 0
	}
	path, backup, changed, err := installServe(ctx, svc)
	if err != nil {
		fmt.Fprintf(stderr, "agentws setup serve: %v\n", err)
		return 1
	}
	if !changed {
		fmt.Fprintf(stdout, "already set up in %s\n", path)
		return 0
	}
	fmt.Fprintf(stdout, "wrote and started %s; serve now runs at login and logs to %s\n", path, serveLog(svc))
	if backup != "" {
		fmt.Fprintf(stdout, "previous file saved as %s\n", backup)
	}
	return 0
}

func serveLog(s serveService) string {
	if s.launchd {
		return s.agent.Log
	}
	return s.unit.Log
}

func installServe(ctx context.Context, s serveService) (path, backup string, changed bool, err error) {
	if s.launchd {
		res, err := launchd.Install(ctx, s.agent, launchd.Exec)
		return res.Path, res.Backup, res.Changed, err
	}
	res, err := systemd.Install(ctx, s.unit, systemd.Exec)
	return res.Path, res.Backup, res.Changed, err
}

func removeServe(ctx context.Context, s serveService) (bool, error) {
	if s.launchd {
		return launchd.Remove(ctx, s.agent, launchd.Exec)
	}
	return systemd.Remove(ctx, s.unit, systemd.Exec)
}

func setupServeService(args []string, env func(string) string, self, userHome string, uid int, goos string) (serveService, bool, error) {
	if goos != "linux" && goos != "darwin" {
		return serveService{}, false, fmt.Errorf("%s is not supported, only linux and macOS", goos)
	}
	fs := flag.NewFlagSet("setup serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remove := fs.Bool("remove", false, "")
	addr := fs.String("addr", "", "")
	cert := fs.String("cert", "", "")
	key := fs.String("key", "", "")
	selfSigned := fs.Bool("self-signed", false, "")
	if err := fs.Parse(args); err != nil {
		return serveService{}, false, err
	}
	if fs.NArg() != 0 {
		return serveService{}, false, errors.New("takes no arguments, only flags")
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	home := env("AGENTWS_HOME")
	if home == "" {
		home = filepath.Join(userHome, ".agentws")
	}
	program := []string{self, "serve"}
	if !*remove {
		var err error
		if program, err = serveProgram(program, given, *addr, *cert, *key, *selfSigned); err != nil {
			return serveService{}, false, err
		}
	}
	svcEnv := map[string]string{"AGENTWS_HOME": home}
	if p := env("PATH"); p != "" {
		svcEnv["PATH"] = p
	}
	log := filepath.Join(home, "serve.log")
	if goos == "darwin" {
		return serveService{launchd: true, agent: launchd.Agent{
			Dir:     filepath.Join(userHome, "Library", "LaunchAgents"),
			Label:   serveAgentLabel,
			Program: program,
			Env:     svcEnv,
			Log:     log,
			UID:     uid,
		}}, *remove, nil
	}
	config := env("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(userHome, ".config")
	}
	return serveService{unit: systemd.Unit{
		Dir:     filepath.Join(config, "systemd", "user"),
		Name:    serveUnitName,
		Program: program,
		Env:     svcEnv,
		Log:     log,
	}}, *remove, nil
}

func serveProgram(program []string, given map[string]bool, addr, cert, key string, selfSigned bool) ([]string, error) {
	tls := selfSigned
	if given["addr"] {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("--addr %q: %w", addr, err)
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, fmt.Errorf("--addr %q: bad port", addr)
		}
		if given["cert"] || given["key"] || selfSigned {
			tls = true
		}
		if !tls && !loopbackHost(host) {
			return nil, fmt.Errorf("--addr %q needs --cert and --key or --self-signed: plain HTTP is only served on loopback", addr)
		}
		program = append(program, "--addr", addr)
	}
	switch {
	case given["cert"] != given["key"]:
		return nil, errors.New("--cert and --key go together")
	case given["cert"] && (cert == "" || key == ""):
		return nil, errors.New("--cert and --key need a file")
	case given["cert"] && selfSigned:
		return nil, errors.New("--self-signed replaces --cert and --key")
	}
	if given["cert"] {
		certPath, err := filepath.Abs(cert)
		if err != nil {
			return nil, err
		}
		keyPath, err := filepath.Abs(key)
		if err != nil {
			return nil, err
		}
		program = append(program, "--cert", certPath, "--key", keyPath)
	}
	if selfSigned {
		program = append(program, "--self-signed")
	}
	return program, nil
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
