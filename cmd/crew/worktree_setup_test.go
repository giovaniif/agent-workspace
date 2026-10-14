package main

import (
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
)

func TestSetupSummaryShowsDurationAndDiskDelta(t *testing.T) {
	cases := []struct {
		name   string
		report app.SetupReport
		want   string
	}{
		{"megabytes", app.SetupReport{Duration: 1500 * time.Millisecond, DiskUsed: 12_300_000}, "done in 1.5s, disk used 12.3 MB"},
		{"gigabytes", app.SetupReport{Duration: 42 * time.Second, DiskUsed: 2_400_000_000}, "done in 42s, disk used 2.4 GB"},
		{"bytes", app.SetupReport{Duration: 20 * time.Millisecond, DiskUsed: 512}, "done in 20ms, disk used 512 B"},
		{"freed space", app.SetupReport{Duration: time.Second, DiskUsed: -5_000_000}, "done in 1s, disk used -5.0 MB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := setupSummary(c.report); got != c.want {
				t.Errorf("setupSummary() = %q, want %q", got, c.want)
			}
		})
	}
}
