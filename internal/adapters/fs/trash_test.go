package fs_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/fs"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var (
	_ app.Trash        = (*fs.Trash)(nil)
	_ app.CleanupAudit = (*fs.AuditLog)(nil)
)

func TestCleanupTrashMovesAtOnceAndDeletesInTheBackground(t *testing.T) {
	tmp := t.TempDir()
	trash := fs.NewTrash(filepath.Join(tmp, "trash"), 2)
	var wts []string
	for _, name := range []string{"a", "x/wt", "y/wt"} {
		wt := filepath.Join(tmp, "wts", name)
		if err := os.MkdirAll(filepath.Join(wt, "node_modules", "x"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, "node_modules", "x", "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		wts = append(wts, wt)
	}
	for _, wt := range wts {
		if err := trash.Move(wt); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("%s still there after Move: %v", wt, err)
		}
	}
	trash.Wait()
	left, err := os.ReadDir(filepath.Join(tmp, "trash"))
	if err != nil || len(left) != 0 {
		t.Errorf("trash after Wait = %v, %v; want empty", left, err)
	}
	if err := trash.Move(filepath.Join(tmp, "missing")); err == nil {
		t.Error("want an error moving a missing path")
	}
}

func TestCleanupTrashPurgesLeftoversFromAnEarlierRun(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "trash")
	if err := os.MkdirAll(filepath.Join(dir, "old-a", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	trash := fs.NewTrash(dir, 2)
	trash.Purge()
	trash.Wait()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left = %v", left)
	}
}

func TestCleanupAuditAppendsOneJSONLinePerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.log")
	log := &fs.AuditLog{Path: path}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	log.Record(app.CleanupRecord{At: at, Path: "/w/a", Branch: "a", Action: domain.CleanupRemove, Reason: "PR #1 merged", Outcome: "removed"})
	log.Record(app.CleanupRecord{At: at, Path: "/w/b", Action: domain.CleanupBackupThenAsk, Outcome: "backed up to /h/b"})
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var got []app.CleanupRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r app.CleanupRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 || got[0].Outcome != "removed" || got[1].Path != "/w/b" || !got[0].At.Equal(at) {
		t.Errorf("records = %+v", got)
	}
}
