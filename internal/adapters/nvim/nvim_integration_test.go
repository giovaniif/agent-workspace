//go:build integration

package nvim_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/nvim"
	"github.com/giovaniif/crew/internal/domain"
)

func startNvim(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "agentws-nvim")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "n.sock")
	cmd := exec.Command("nvim", "--headless", "--clean", "--listen", sock)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("nvim never listened")
	return ""
}

func ask(t *testing.T, sock, expr string) string {
	t.Helper()
	out, err := exec.Command("nvim", "--server", sock, "--remote-expr", expr).Output()
	if err != nil {
		t.Fatalf("nvim --remote-expr %s: %v", expr, err)
	}
	return strings.TrimSpace(string(out))
}

func TestEditorOpensAFileWithSpacesAndQuotesAtALine(t *testing.T) {
	sock := startNvim(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "it's a file.txt")
	if err := os.WriteFile(file, []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (nvim.Editor{}).Eval(context.Background(), sock, domain.NvimOpenExpr(file, 4)); err != nil {
		t.Fatal(err)
	}
	if got := ask(t, sock, `expand('%:p') . ':' . line('.')`); got != file+":4" {
		t.Fatalf("nvim is at %q; want %s:4", got, file)
	}
}

func TestEditorReportsAnUnreachableNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	err := (nvim.Editor{}).Eval(context.Background(), filepath.Join(t.TempDir(), "gone.sock"), "1")
	if err == nil {
		t.Fatal("Eval succeeded against a socket nobody listens on")
	}
}
