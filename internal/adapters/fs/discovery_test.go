package fs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/fs"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.WorkspaceFS = fs.FS{}

func mkRepo(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkWorktree(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(repos []domain.Repo) []string {
	var out []string
	for _, r := range repos {
		out = append(out, r.Name)
	}
	return out
}

func TestDiscoveryFindsFourRepos(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, n := range []string{"api", "web", "worker"} {
		mkRepo(t, filepath.Join(root, n))
	}
	mkRepo(t, filepath.Join(outside, "shared"))
	if err := os.Symlink(filepath.Join(outside, "shared"), filepath.Join(root, "shared-link")); err != nil {
		t.Fatal(err)
	}
	mkWorktree(t, filepath.Join(root, "api-feature"))
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := app.DiscoverWorkspace(fs.FS{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != domain.WorkspaceOrchestration {
		t.Errorf("kind = %q, want orchestration", ws.Kind)
	}
	got := names(ws.Repos)
	want := []string{"api", "shared-link", "web", "worker"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("repos = %v, want %v", got, want)
	}
	for _, r := range ws.Repos {
		if r.Name == "shared-link" && r.Path != filepath.Join(root, "shared-link") {
			t.Errorf("symlinked repo path = %q, want the link path", r.Path)
		}
	}
}

func TestDiscoverySingleRepoDetected(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	mkRepo(t, filepath.Join(root, "vendor", "inner"))
	ws, err := app.DiscoverWorkspace(fs.FS{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != domain.WorkspaceSingle {
		t.Errorf("kind = %q, want single", ws.Kind)
	}
	if len(ws.Repos) != 1 || ws.Repos[0].Path != root {
		t.Errorf("repos = %+v, want just the root", ws.Repos)
	}
}

func TestDiscoveryOnlyLooksOneLevelDeep(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "group", "nested"))
	ws, err := app.DiscoverWorkspace(fs.FS{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Repos) != 0 {
		t.Errorf("repos = %v, want none", names(ws.Repos))
	}
}

func TestDiscoveryIgnoresBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	mkRepo(t, filepath.Join(root, "api"))
	ws, err := app.DiscoverWorkspace(fs.FS{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ws.Repos); fmt.Sprint(got) != "[api]" {
		t.Errorf("repos = %v, want [api]", got)
	}
}

func TestDiscoveryRejectsFileAndMissingPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, filepath.Join(dir, "missing")} {
		if _, err := app.DiscoverWorkspace(fs.FS{}, p, nil); err == nil {
			t.Errorf("DiscoverWorkspace(%q) succeeded", p)
		}
	}
}

func BenchmarkDiscovery(b *testing.B) {
	const repos = 15
	root := b.TempDir()
	for i := range repos {
		mkRepo(b, filepath.Join(root, fmt.Sprintf("repo%02d", i)))
	}
	mkWorktree(b, filepath.Join(root, "wt"))
	b.ResetTimer()
	start := time.Now()
	for range b.N {
		ws, err := app.DiscoverWorkspace(fs.FS{}, root, nil)
		if err != nil || len(ws.Repos) != repos {
			b.Fatalf("found %d repos, err %v", len(ws.Repos), err)
		}
	}
	perOp := time.Since(start) / time.Duration(b.N)
	if perOp > 300*time.Millisecond {
		b.Fatalf("discovery over %d repos took %v, budget 300ms", repos, perOp)
	}
}
