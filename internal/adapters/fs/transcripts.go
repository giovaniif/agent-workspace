package fs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type Transcripts struct{}

func (Transcripts) Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (Transcripts) ReadAt(path string, off, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return buf[:got], err
}

func (Transcripts) Watch(ctx context.Context, path string) (<-chan struct{}, error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	onFile := w.Add(path) == nil
	if !onFile {
		if err := w.Add(dir); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		defer func() { _ = w.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != path {
					continue
				}
				switch {
				case !onFile && ev.Has(fsnotify.Create):
					if w.Add(path) == nil {
						onFile = true
						_ = w.Remove(dir)
					}
				case onFile && (ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)):
					if w.Add(dir) == nil {
						onFile = false
					}
				}
				select {
				case out <- struct{}{}:
				default:
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, nil
}
