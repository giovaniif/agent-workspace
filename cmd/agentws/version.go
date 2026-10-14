package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/github"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func runVersion(current, commit string, latest func(context.Context) (string, error), w io.Writer) int {
	fmt.Fprintf(w, "agentws %s (commit %s)\n", current, commit)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tag, err := latest(ctx)
	if err != nil || !domain.NewerRelease(current, tag) {
		return 0
	}
	fmt.Fprintf(w, "agentws %s is available. Upgrade (see the README), then run `agentws daemon stop` so the new binary starts a new daemon.\n", tag)
	return 0
}

var errUpdateCheckOff = errors.New("update check off")

func latestRelease(ctx context.Context) (string, error) {
	if domain.Env(os.Getenv, "NO_UPDATE_CHECK") != "" {
		return "", errUpdateCheckOff
	}
	home, err := rpc.Home()
	if err != nil {
		return "", err
	}
	c := github.ReleaseChecker{URL: github.ReleasesURL, CachePath: filepath.Join(home, "release.json")}
	return c.Latest(ctx)
}
