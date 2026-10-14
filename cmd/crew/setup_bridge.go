package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/adapters/launchd"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const setupBridgeUsage = "usage: crew setup bridge [--remote-bin path] [--remove] <ssh host>"

func runSetupBridge(args []string, stdout, stderr io.Writer, env func(string) string, self string) int {
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "crew setup bridge: %v\n", err)
		return 1
	}
	a, remove, err := setupBridgeAgent(args, env, self, userHome, os.Getuid())
	if err != nil {
		fmt.Fprintf(stderr, "crew setup bridge: %v\n%s\n", err, setupBridgeUsage)
		return 2
	}
	ctx := context.Background()
	if remove {
		removed, err := launchd.Remove(ctx, a, launchd.Exec)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "crew setup bridge: %v\n", err)
			return 1
		case removed:
			fmt.Fprintf(stdout, "stopped and removed %s\n", a.Label)
		default:
			fmt.Fprintf(stdout, "no bridge agent %s to remove\n", a.Label)
		}
		return 0
	}
	res, err := launchd.Install(ctx, a, launchd.Exec)
	if err != nil {
		fmt.Fprintf(stderr, "crew setup bridge: %v\n", err)
		return 1
	}
	if !res.Changed {
		fmt.Fprintf(stdout, "already set up in %s\n", res.Path)
		return 0
	}
	fmt.Fprintf(stdout, "wrote and loaded %s; the bridge now runs at login and logs to %s\n", res.Path, a.Log)
	if res.Backup != "" {
		fmt.Fprintf(stdout, "previous file saved as %s\n", res.Backup)
	}
	return 0
}

func setupBridgeAgent(args []string, env func(string) string, self, userHome string, uid int) (launchd.Agent, bool, error) {
	fs := flag.NewFlagSet("setup bridge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remote := fs.String("remote-bin", "agentws", "")
	remove := fs.Bool("remove", false, "")
	if err := fs.Parse(args); err != nil {
		return launchd.Agent{}, false, err
	}
	if fs.NArg() != 1 {
		return launchd.Agent{}, false, errors.New("needs exactly one ssh host")
	}
	host := fs.Arg(0)
	home := domain.Env(env, "HOME")
	if home == "" {
		home = rpc.DefaultHome(userHome)
	}
	program := []string{self, "notify", "bridge"}
	if *remote != "agentws" {
		program = append(program, "--remote-bin", *remote)
	}
	agentEnv := map[string]string{domain.EnvName("HOME"): home}
	if p := env("PATH"); p != "" {
		agentEnv["PATH"] = p
	}
	slug := labelSafe(host)
	return launchd.Agent{
		Dir:     filepath.Join(userHome, "Library", "LaunchAgents"),
		Label:   "dev.crew.bridge." + slug,
		Program: append(program, host),
		Env:     agentEnv,
		Log:     filepath.Join(home, "bridge-"+slug+".log"),
		UID:     uid,
	}, *remove, nil
}

func labelSafe(host string) string {
	slug := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, host)
	if slug != host {
		sum := sha256.Sum256([]byte(host))
		slug += "-" + hex.EncodeToString(sum[:])[:8]
	}
	return slug
}
