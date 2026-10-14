//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	wsfs "github.com/giovaniif/crew/internal/adapters/fs"
	gitadapter "github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/adapters/procs"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

type cleanupRig struct {
	tmp, repo string
	home      string
	trash     *wsfs.Trash
	cleanup   *app.Cleanup
}

func newCleanupRig(t *testing.T) cleanupRig {
	t.Helper()
	for _, bin := range []string{"git", "lsof", "tar"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(tmp, "seed")
	newRepo(t, seed)
	if err := os.WriteFile(filepath.Join(seed, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-q", "-m", "ignore")
	git(t, tmp, "clone", "-q", "--bare", seed, filepath.Join(tmp, "origin.git"))
	git(t, tmp, "clone", "-q", filepath.Join(tmp, "origin.git"), "api")
	r := cleanupRig{tmp: tmp, repo: filepath.Join(tmp, "api"), home: filepath.Join(tmp, "home")}
	r.trash = wsfs.NewTrash(filepath.Join(r.home, "trash"), 4)
	later := func() time.Time { return time.Now().Add(domain.CleanupGrace + time.Hour) }
	r.cleanup = app.NewCleanup(gitadapter.Worktrees{}, procs.Table{}, r.trash,
		&wsfs.AuditLog{Path: filepath.Join(r.home, "cleanup.log")}, filepath.Join(r.home, "backups"), later)
	return r
}

func (r cleanupRig) add(t *testing.T, name string, args ...string) domain.Worktree {
	t.Helper()
	path := filepath.Join(r.tmp, name)
	git(t, r.repo, append([]string{"worktree", "add", "-q"}, append(args, path)...)...)
	branch := ""
	if len(args) > 1 && args[0] == "-b" {
		branch = args[1]
	}
	return domain.Worktree{ID: path, Repo: r.repo, Path: path, Branch: branch}
}

func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", file)
}

func refs(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCleanupExecTwentyWorktreesInMixedStates(t *testing.T) {
	r := newCleanupRig(t)
	var wts []domain.Worktree
	want := map[string]string{}
	expect := func(w domain.Worktree, outcome string) {
		wts = append(wts, w)
		want[w.Path] = outcome
	}

	var mergedBranches []domain.Worktree
	for i := range 8 {
		w := r.add(t, fmt.Sprintf("merged-%d", i), "-b", fmt.Sprintf("merged-%d", i))
		commitIn(t, w.Path, fmt.Sprintf("m%d.txt", i))
		mergedBranches = append(mergedBranches, w)
	}
	for _, w := range mergedBranches {
		git(t, r.repo, "merge", "-q", "--no-edit", w.Branch)
	}
	git(t, r.repo, "push", "-q", "origin", "main")
	git(t, r.repo, "fetch", "-q")
	for _, w := range mergedBranches[:6] {
		expect(w, "removed")
	}
	for i := range 3 {
		w := r.add(t, fmt.Sprintf("squashed-%d", i), "-b", fmt.Sprintf("squashed-%d", i))
		commitIn(t, w.Path, fmt.Sprintf("s%d.txt", i))
		w.PR = &domain.PullRequest{Number: 100 + i, Head: w.Branch, State: domain.PRMerged}
		expect(w, "removed")
	}
	for i := range 3 {
		w := r.add(t, fmt.Sprintf("dirty-%d", i), "-b", fmt.Sprintf("dirty-%d", i))
		if err := os.WriteFile(filepath.Join(w.Path, "wip.txt"), []byte("wip"), 0o644); err != nil {
			t.Fatal(err)
		}
		w.PR = &domain.PullRequest{Number: 200 + i, Head: w.Branch, State: domain.PRMerged}
		expect(w, "backed up")
	}
	for i := range 3 {
		w := r.add(t, fmt.Sprintf("open-%d", i), "-b", fmt.Sprintf("open-%d", i))
		commitIn(t, w.Path, fmt.Sprintf("o%d.txt", i))
		w.PR = &domain.PullRequest{Number: 300 + i, Head: w.Branch, State: domain.PROpen}
		expect(w, "kept")
	}
	for i := range 2 {
		w := r.add(t, fmt.Sprintf("detached-%d", i), "--detach")
		commitIn(t, w.Path, fmt.Sprintf("d%d.txt", i))
		expect(w, "backed up")
	}
	for _, w := range mergedBranches[6:] {
		sleeper := exec.Command("sleep", "60")
		sleeper.Dir = w.Path
		if err := sleeper.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
		expect(w, "kept")
	}
	recent := r.add(t, "recent", "-b", "recent")
	recent.PR = &domain.PullRequest{Number: 400, Head: "recent", State: domain.PRMerged}
	expect(recent, "kept")
	if len(wts) != 20 {
		t.Fatalf("built %d worktrees, want 20", len(wts))
	}

	before := refs(t, r.repo)
	activity := func(w domain.Worktree) app.SessionActivity {
		if w.Path == recent.Path {
			return app.SessionActivity{LastActivity: time.Now().Add(domain.CleanupGrace + time.Hour)}
		}
		return app.SessionActivity{}
	}
	results := r.cleanup.Execute(context.Background(), wts, activity)
	r.trash.Wait()

	for _, res := range results {
		p := res.Decision.Worktree.Path
		if !strings.HasPrefix(res.Outcome, want[p]) {
			t.Errorf("%s: outcome %q (%s), want %q", filepath.Base(p), res.Outcome, res.Decision.Reason, want[p])
		}
		if gone := !exists(p); gone != (want[p] == "removed") {
			t.Errorf("%s: gone = %v, want %v", filepath.Base(p), gone, want[p] == "removed")
		}
	}
	after := refs(t, r.repo)
	for _, ref := range before {
		if !contains(after, ref) {
			t.Errorf("branch %q lost or moved", ref)
		}
	}
	for i := range 2 {
		if !contains(after, "refs/heads/backup/wt-detached-"+fmt.Sprint(i)) {
			t.Errorf("no backup branch for detached-%d in %v", i, after)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(r.home, "backups", "*", "*", "status.txt"))
	if len(backups) != 5 {
		t.Errorf("backups = %v, want 5 (3 dirty, 2 detached)", backups)
	}
	patches, _ := filepath.Glob(filepath.Join(r.home, "backups", "*", "dirty-0", "untracked.tar"))
	if len(patches) != 1 {
		t.Errorf("dirty-0 untracked.tar missing: %v", patches)
	}
	list := gitOutput(t, r.repo, "worktree", "list", "--porcelain")
	for p, outcome := range want {
		if strings.Contains(list, "worktree "+p+"\n") == (outcome == "removed") {
			t.Errorf("%s listed = %v after cleanup", filepath.Base(p), outcome != "removed")
		}
	}
	if trash, _ := os.ReadDir(filepath.Join(r.home, "trash")); len(trash) != 0 {
		t.Errorf("trash not emptied: %v", trash)
	}
	log, err := os.ReadFile(filepath.Join(r.home, "cleanup.log"))
	if err != nil || strings.Count(string(log), "\n") != 14 {
		t.Errorf("audit log has %d lines, want 14 (9 removed, 5 backed up): %v", strings.Count(string(log), "\n"), err)
	}
}

func contains(refs []string, prefix string) bool {
	for _, r := range refs {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCleanupExecFiftyGigabyteWorktreesReturnWithinTenSeconds(t *testing.T) {
	r := newCleanupRig(t)
	var wts []domain.Worktree
	for i := range 50 {
		w := r.add(t, fmt.Sprintf("big-%02d", i), "-b", fmt.Sprintf("big-%02d", i))
		if err := os.MkdirAll(filepath.Join(w.Path, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(w.Path, "node_modules", "big.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(1 << 30); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		wts = append(wts, w)
	}
	start := time.Now()
	results := r.cleanup.Execute(context.Background(), wts, func(domain.Worktree) app.SessionActivity { return app.SessionActivity{} })
	took := time.Since(start)
	var removed []string
	for _, res := range results {
		if res.Outcome == "removed" {
			removed = append(removed, res.Decision.Worktree.Path)
		}
	}
	sort.Strings(removed)
	if len(removed) != 50 {
		t.Fatalf("removed %d of 50: %+v", len(removed), results[0])
	}
	if took > 10*time.Second {
		t.Errorf("Execute took %v, want < 10s", took)
	}
	t.Logf("50 x 1 GB worktrees: control back in %v", took)
	r.trash.Wait()
	if trash, _ := os.ReadDir(filepath.Join(r.home, "trash")); len(trash) != 0 {
		t.Errorf("trash not emptied: %d left", len(trash))
	}
}
