package procs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

var _ app.WorktreeHolders = Table{}

func (Table) Holders(ctx context.Context, paths []string) (map[string][]string, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-n", "-P", "-w", "-d", "cwd", "-F", "pcn").Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(out) == 0 {
			return nil, fmt.Errorf("lsof: %w", err)
		}
	}
	resolved := map[string]string{}
	for _, p := range paths {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			resolved[p] = r
		}
	}
	return holders(out, paths, resolved), nil
}

func holders(out []byte, paths []string, resolved map[string]string) map[string][]string {
	found := map[string][]string{}
	var pid, command string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		v := string(line[1:])
		switch line[0] {
		case 'p':
			pid, command = v, ""
		case 'c':
			command = v
		case 'n':
			for _, p := range paths {
				if inside(v, p) || (resolved[p] != "" && inside(v, resolved[p])) {
					found[p] = append(found[p], fmt.Sprintf("%s (pid %s)", command, pid))
				}
			}
		}
	}
	return found
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
