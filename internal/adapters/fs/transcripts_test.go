package fs_test

import (
	"context"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/fs"
	"github.com/giovaniif/agent-workspace/internal/app"
)

var (
	_ app.TranscriptFiles   = fs.Transcripts{}
	_ app.TranscriptWatcher = fs.Transcripts{}
)

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func notified(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatalf("%s: watch closed", what)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: no notification", what)
	}
}

func TestTranscriptFilesReadSizeAndRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, "line one\nline two\n")
	var files fs.Transcripts
	size, err := files.Size(path)
	if err != nil || size != 18 {
		t.Fatalf("size %d %v", size, err)
	}
	b, err := files.ReadAt(path, 5, 8)
	if err != nil || string(b) != "one\nline" {
		t.Fatalf("read %q %v", b, err)
	}
	b, err = files.ReadAt(path, 14, 100)
	if err != nil || string(b) != "two\n" {
		t.Fatalf("read past the end %q %v", b, err)
	}
	missing := filepath.Join(t.TempDir(), "none.jsonl")
	if _, err := files.Size(missing); !errors.Is(err, iofs.ErrNotExist) {
		t.Fatalf("size of a missing file: %v", err)
	}
	if _, err := files.ReadAt(missing, 0, 1); !errors.Is(err, iofs.ErrNotExist) {
		t.Fatalf("read of a missing file: %v", err)
	}
}

func TestTranscriptWatchNotifiesOnAppendsAndClosesWithItsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, "a\n")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := fs.Transcripts{}.Watch(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "b\n")
	notified(t, ch, "append")
	cancel()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("watch not closed after cancel")
		}
	}
}

func TestTranscriptWatchNotifiesWhenTheFileIsCreatedAndThenWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := fs.Transcripts{}.Watch(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(filepath.Dir(path), "other.jsonl"), "x\n")
	appendTo(t, path, "a\n")
	notified(t, ch, "create")
	for len(ch) > 0 {
		<-ch
	}
	time.Sleep(50 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}
	appendTo(t, path, "b\n")
	notified(t, ch, "append after create")
}

func TestTranscriptWatchOfAMissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone", "s.jsonl")
	if _, err := (fs.Transcripts{}).Watch(context.Background(), path); err == nil {
		t.Fatal("watch of a missing directory succeeded")
	}
}
