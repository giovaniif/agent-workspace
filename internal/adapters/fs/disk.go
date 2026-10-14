package fs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/giovaniif/crew/internal/app"
)

var (
	_ app.Sizer          = Du{}
	_ app.VolumeStat     = Volume{}
	_ app.CleanupHistory = (*AuditLog)(nil)
)

type Du struct{}

func (Du) Size(ctx context.Context, path string) (int64, error) {
	out, err := exec.CommandContext(ctx, "du", "-sk", "-P", path).Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(out) == 0 {
			return 0, fmt.Errorf("du %s: %w", path, err)
		}
	}
	kib, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	n, err := strconv.ParseInt(kib, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("du %s: unexpected output %q", path, out)
	}
	return n * 1024, nil
}

type Volume struct{}

func (Volume) Stat(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return uint64(st.Bavail) * bsize, uint64(st.Blocks) * bsize, nil
}

func (a *AuditLog) Recent(n int) []app.CleanupRecord {
	a.mu.Lock()
	data, err := os.ReadFile(a.Path)
	a.mu.Unlock()
	if err != nil {
		return nil
	}
	var all []app.CleanupRecord
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r app.CleanupRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			all = append(all, r)
		}
	}
	out := make([]app.CleanupRecord, 0, min(n, len(all)))
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out
}
