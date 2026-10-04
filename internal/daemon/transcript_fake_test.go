package daemon_test

import (
	"context"
	"io/fs"
	"sync"
)

type memTranscripts struct {
	mu      sync.Mutex
	files   map[string][]byte
	touched int
	watches int
	active  map[string]map[chan struct{}]bool
}

func newMemTranscripts() *memTranscripts {
	return &memTranscripts{files: map[string][]byte{}, active: map[string]map[chan struct{}]bool{}}
}

func (m *memTranscripts) write(path, s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = append(m.files[path], s...)
	for ch := range m.active[path] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (m *memTranscripts) counts() (touched, watches, active int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, chs := range m.active {
		active += len(chs)
	}
	return m.touched, m.watches, active
}

func (m *memTranscripts) Size(path string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched++
	b, ok := m.files[path]
	if !ok {
		return 0, fs.ErrNotExist
	}
	return int64(len(b)), nil
}

func (m *memTranscripts) ReadAt(path string, off, n int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched++
	b, ok := m.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b[off:min(off+n, int64(len(b)))]...), nil
}

func (m *memTranscripts) Watch(ctx context.Context, path string) (<-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched++
	m.watches++
	ch := make(chan struct{}, 1)
	if m.active[path] == nil {
		m.active[path] = map[chan struct{}]bool{}
	}
	m.active[path][ch] = true
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.active[path], ch)
		m.mu.Unlock()
	}()
	return ch, nil
}
