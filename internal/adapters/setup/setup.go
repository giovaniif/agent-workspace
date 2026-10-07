package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

var (
	_ app.RecipeSource  = Recipes{}
	_ app.SetupFS       = FS{}
	_ app.CommandRunner = Shell{}
)

const recipeFile = ".agentws.toml"

type Recipes struct{}

type recipeDoc struct {
	Setup struct {
		Copy []string `toml:"copy"`
		Link []string `toml:"link"`
		Run  []string `toml:"run"`
		Deps string   `toml:"deps"`
	} `toml:"setup"`
}

func (Recipes) Load(repoDir string) (domain.Recipe, bool, error) {
	file := filepath.Join(repoDir, recipeFile)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Recipe{}, false, nil
	}
	if err != nil {
		return domain.Recipe{}, false, err
	}
	var doc recipeDoc
	meta, err := toml.Decode(string(data), &doc)
	if err != nil {
		return domain.Recipe{}, false, fmt.Errorf("%s: %w", recipeFile, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return domain.Recipe{}, false, fmt.Errorf("%s: unknown key %s", recipeFile, undecoded[0])
	}
	s := doc.Setup
	return domain.Recipe{Copy: s.Copy, Link: s.Link, Run: s.Run, Deps: domain.DepsMode(s.Deps)}, true, nil
}

type FS struct{}

func (FS) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (FS) Copy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return cp(context.Background(), "-R", src, dst)
}

func (FS) Symlink(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}

func (FS) CloneTree(ctx context.Context, src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return cp(ctx, "-c", "-R", src, dst)
	}
	return cp(ctx, "--reflink=auto", "-R", src, dst)
}

func cp(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "cp", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (FS) Lockfile(dir string) (domain.Lockfile, error) {
	for _, name := range domain.LockfileNames {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return domain.Lockfile{}, err
		}
		sum := sha256.Sum256(data)
		return domain.Lockfile{Name: name, Hash: hex.EncodeToString(sum[:])}, nil
	}
	return domain.Lockfile{}, nil
}

func (FS) FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:gosec
}

type Shell struct {
	Out io.Writer
}

func (s Shell) Run(ctx context.Context, dir string, argv ...string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var tail tailBuffer
	out := io.Writer(&tail)
	if s.Out != nil {
		out = io.MultiWriter(s.Out, &tail)
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if shown := strings.TrimSpace(tail.String()); shown != "" {
			return fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "), err, shown)
		}
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return nil
}

const shellTailBytes = 4096

type tailBuffer struct {
	data []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > shellTailBytes {
		t.data = t.data[len(t.data)-shellTailBytes:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	return string(t.data)
}
