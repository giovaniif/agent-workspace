package loginshell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	start = "\x1ecrew-login-path\x1e"
	end   = "\x1fcrew-login-path\x1f"
)

func Path(ctx context.Context, shell string) (string, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", `printf '%s%s%s' '`+start+`' "$PATH" '`+end+`'`)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	path, ok := between(string(out))
	if err != nil && (!errors.Is(err, exec.ErrWaitDelay) || !ok) {
		return "", fmt.Errorf("login shell %s: %w", shell, err)
	}
	if !ok {
		return "", errors.New("login shell " + shell + ": no PATH in its output")
	}
	return path, nil
}

func between(out string) (string, bool) {
	_, rest, ok := strings.Cut(out, start)
	if !ok {
		return "", false
	}
	path, _, ok := strings.Cut(rest, end)
	return path, ok
}
