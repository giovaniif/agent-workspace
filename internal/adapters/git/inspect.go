package git

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/giovaniif/crew/internal/app"
)

var _ app.RepoInspector = Inspector{}

type Inspector struct{}

func (Inspector) Inspect(ctx context.Context, path string) (app.RepoFacts, error) {
	status, err := output(ctx, path, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return app.RepoFacts{}, err
	}
	branch, changed := parseStatus(status)
	facts := app.RepoFacts{Branch: branch, ChangedFiles: changed}
	if head, err := output(ctx, path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		facts.DefaultBranch = parseDefaultBranch(string(head))
	}
	return facts, nil
}

func output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(isolated(cmd.Environ()), "GIT_OPTIONAL_LOCKS=0")
	return cmd.Output()
}

func parseStatus(out []byte) (branch string, changed int) {
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		rec := string(records[i])
		switch {
		case strings.HasPrefix(rec, "# branch.head "):
			if b := strings.TrimPrefix(rec, "# branch.head "); b != "(detached)" {
				branch = b
			}
		case strings.HasPrefix(rec, "1 "), strings.HasPrefix(rec, "u "), strings.HasPrefix(rec, "? "):
			changed++
		case strings.HasPrefix(rec, "2 "):
			changed++
			i++
		}
	}
	return branch, changed
}

func parseDefaultBranch(symbolicRef string) string {
	ref := strings.TrimSpace(symbolicRef)
	_, branch, ok := strings.Cut(ref, "/")
	if !ok {
		return ""
	}
	return branch
}
