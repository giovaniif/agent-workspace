//go:build integration

package fs_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/fs"
	"github.com/giovaniif/crew/internal/app"
)

var _ app.Sizer = fs.Du{}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiskDuCountsAHardlinkedFileOnce(t *testing.T) {
	const mib = 1 << 20
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "a"), 4*mib)
	writeFile(t, filepath.Join(plain, "b"), 4*mib)

	linked := t.TempDir()
	writeFile(t, filepath.Join(linked, "a"), 4*mib)
	if err := os.Link(filepath.Join(linked, "a"), filepath.Join(linked, "b")); err != nil {
		t.Fatal(err)
	}

	two, err := fs.Du{}.Size(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	one, err := fs.Du{}.Size(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if two < 8*mib || one < 4*mib || one >= 6*mib {
		t.Errorf("two files = %d, hardlinked pair = %d; want the pair to cost about half of two copies", two, one)
	}
}

func TestDiskDuDoesNotFollowSymlinks(t *testing.T) {
	const mib = 1 << 20
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "big"), 16*mib)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "small"), 1024)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	size, err := fs.Du{}.Size(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if size >= mib {
		t.Errorf("size = %d, want under 1 MiB: the symlink's target is not this directory's", size)
	}
}

func TestDiskDuFailsForAMissingDirectory(t *testing.T) {
	if _, err := (fs.Du{}).Size(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("want an error")
	}
}
