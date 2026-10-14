package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giovaniif/crew/internal/app"
)

var (
	_ app.Trash        = (*Trash)(nil)
	_ app.CleanupAudit = (*AuditLog)(nil)
)

type Trash struct {
	dir  string
	sem  chan struct{}
	wg   sync.WaitGroup
	next atomic.Uint64
}

func NewTrash(dir string, parallelism int) *Trash {
	return &Trash{dir: dir, sem: make(chan struct{}, parallelism)}
}

func (t *Trash) Move(path string) error {
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(t.dir, fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), t.next.Add(1), filepath.Base(path)))
	if err := os.Rename(path, dest); err != nil {
		return err
	}
	t.delete(dest)
	return nil
}

func (t *Trash) Purge() {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		t.delete(filepath.Join(t.dir, e.Name()))
	}
}

func (t *Trash) Wait() { t.wg.Wait() }

func (t *Trash) delete(path string) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.sem <- struct{}{}
		defer func() { <-t.sem }()
		_ = os.RemoveAll(path)
	}()
}

type AuditLog struct {
	Path string
	mu   sync.Mutex
}

func (a *AuditLog) Record(r app.CleanupRecord) {
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(a.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}
