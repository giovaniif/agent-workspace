package daemon_test

import (
	"context"
	"errors"
	"sync"

	"github.com/giovaniif/crew/internal/app"
)

type gatedSizer struct {
	open  chan struct{}
	sizes map[string]int64
}

func newGatedSizer(sizes map[string]int64) *gatedSizer {
	return &gatedSizer{open: make(chan struct{}), sizes: sizes}
}

func (g *gatedSizer) Size(_ context.Context, path string) (int64, error) {
	<-g.open
	size, ok := g.sizes[path]
	if !ok {
		return 0, errors.New("no such directory")
	}
	return size, nil
}

type fakeVolume struct{ free, total uint64 }

func (v fakeVolume) Stat(string) (uint64, uint64, error) { return v.free, v.total, nil }

type fakeHistory struct {
	mu      sync.Mutex
	records []app.CleanupRecord
}

func (h *fakeHistory) Recent(n int) []app.CleanupRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.records[:min(n, len(h.records))]
}
