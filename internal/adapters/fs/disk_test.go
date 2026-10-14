package fs_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/fs"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var (
	_ app.VolumeStat     = fs.Volume{}
	_ app.CleanupHistory = (*fs.AuditLog)(nil)
)

func TestDiskVolumeReportsFreeAndTotalBytes(t *testing.T) {
	free, total, err := fs.Volume{}.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 || free == 0 || free > total {
		t.Errorf("free %d, total %d; want 0 < free <= total", free, total)
	}
	if _, _, err := (fs.Volume{}).Stat(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("want an error for a missing path")
	}
}

func TestDiskAuditRecentReturnsTheNewestRecordsNewestFirstAndSkipsBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.log")
	log := &fs.AuditLog{Path: path}
	if got := log.Recent(5); len(got) != 0 {
		t.Errorf("Recent with no log = %v, want none", got)
	}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		log.Record(app.CleanupRecord{At: at.Add(time.Duration(i) * time.Minute), Path: "/w/" + strconv.Itoa(i), Action: domain.CleanupRemove, Outcome: "removed"})
		if i == 2 {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteString("not json\n")
			_ = f.Close()
		}
	}
	got := log.Recent(3)
	if len(got) != 3 || got[0].Path != "/w/4" || got[1].Path != "/w/3" || got[2].Path != "/w/2" {
		t.Errorf("Recent(3) = %+v, want /w/4, /w/3, /w/2", got)
	}
	if all := log.Recent(50); len(all) != 5 {
		t.Errorf("Recent(50) has %d records, want the 5 valid ones", len(all))
	}
}
