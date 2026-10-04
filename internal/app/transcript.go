package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const (
	DefaultTranscriptSpan  = 256 << 10
	DefaultTranscriptReach = 1 << 20
)

var (
	ErrBadCursor          = errors.New("cursor is not at a transcript line boundary")
	ErrNoTranscriptParser = errors.New("no transcript parser for this harness")
)

type TranscriptParser interface {
	Parse(data []byte, base int64) ([]domain.Message, int64)
}

type TranscriptFiles interface {
	Size(path string) (int64, error)
	ReadAt(path string, off, n int64) ([]byte, error)
}

type TranscriptWatcher interface {
	Watch(ctx context.Context, path string) (<-chan struct{}, error)
}

type Transcripts struct {
	Files   TranscriptFiles
	Parsers map[domain.Harness]func() TranscriptParser
	Span    int64
	Reach   int64
}

type TranscriptPage struct {
	Messages []domain.Message
	Before   int64
}

func (t Transcripts) span() int64 {
	if t.Span > 0 {
		return t.Span
	}
	return DefaultTranscriptSpan
}

func (t Transcripts) reach() int64 {
	if t.Reach > 0 {
		return t.Reach
	}
	return DefaultTranscriptReach
}

func (t Transcripts) size(path string) (int64, bool, error) {
	if path == "" {
		return 0, false, nil
	}
	size, err := t.Files.Size(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	return size, err == nil, err
}

func (t Transcripts) checkLine(path string, at, size int64) error {
	if at > size {
		return ErrBadCursor
	}
	if at == 0 {
		return nil
	}
	b, err := t.Files.ReadAt(path, at-1, 1)
	if err != nil {
		return err
	}
	if len(b) != 1 || b[0] != '\n' {
		return ErrBadCursor
	}
	return nil
}

func (t Transcripts) readLines(path string, start, end int64) ([]byte, int64, bool, error) {
	data, err := t.Files.ReadAt(path, start, end-start)
	if err != nil || start == 0 {
		return data, start, true, err
	}
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return nil, start, false, nil
	}
	return data[i+1:], start + int64(i) + 1, true, nil
}

func (t Transcripts) Page(h domain.Harness, path string, before int64, limit int) (TranscriptPage, error) {
	newParser, ok := t.Parsers[h]
	if !ok {
		return TranscriptPage{}, ErrNoTranscriptParser
	}
	limit = domain.TranscriptPageLimit(limit)
	size, exists, err := t.size(path)
	if err != nil || !exists {
		return TranscriptPage{}, err
	}
	end := size
	if before > 0 {
		if err := t.checkLine(path, before, size); err != nil {
			return TranscriptPage{}, err
		}
		end = before
	}
	for span := t.span(); ; span *= 2 {
		start := max(0, end-span)
		data, off, aligned, err := t.readLines(path, start, end)
		if err != nil {
			return TranscriptPage{}, err
		}
		if !aligned {
			continue
		}
		p := newParser()
		msgs, parsed := p.Parse(data, off)
		var calls domain.ToolCalls
		msgs = calls.Resolve(msgs)
		if len(msgs) < limit && start > 0 {
			continue
		}
		page := TranscriptPage{Messages: domain.NewestPage(msgs, limit)}
		if len(page.Messages) < len(msgs) || start > 0 {
			page.Before = lineStart(data, off, page.Messages[0].Cursor)
		}
		if err := t.finishCalls(p, path, parsed, size, page.Messages); err != nil {
			return TranscriptPage{}, err
		}
		return page, nil
	}
}

func lineStart(data []byte, off, end int64) int64 {
	lines, _ := domain.TranscriptLines(data, off)
	for _, l := range lines {
		if l.End == end {
			return l.Start
		}
	}
	return off
}

func (t Transcripts) finishCalls(p TranscriptParser, path string, from, size int64, page []domain.Message) error {
	running := map[string]int{}
	for i, m := range page {
		if m.Tool != nil && m.Tool.Status == domain.ToolRunning && m.ID != "" {
			running[m.ID] = i
		}
	}
	if len(running) == 0 || from >= size {
		return nil
	}
	data, err := t.Files.ReadAt(path, from, min(size, from+t.reach())-from)
	if err != nil {
		return err
	}
	msgs, _ := p.Parse(data, from)
	for _, m := range msgs {
		if i, ok := running[m.ID]; ok && m.Tool != nil && m.Tool.Name != "" {
			page[i] = m
		}
	}
	return nil
}

func (t Transcripts) primed(newParser func() TranscriptParser, path string, at int64) (TranscriptParser, error) {
	p := newParser()
	if at == 0 {
		return p, nil
	}
	data, off, aligned, err := t.readLines(path, max(0, at-t.reach()), at)
	if err != nil {
		return nil, err
	}
	if aligned {
		p.Parse(data, off)
	}
	return p, nil
}

type TranscriptTail struct {
	t         Transcripts
	path      string
	newParser func() TranscriptParser
	parser    TranscriptParser
	calls     domain.ToolCalls
	offset    int64
}

func (t Transcripts) Tail(h domain.Harness, path string, after int64) (*TranscriptTail, error) {
	newParser, ok := t.Parsers[h]
	if !ok {
		return nil, ErrNoTranscriptParser
	}
	size, _, err := t.size(path)
	if err != nil {
		return nil, err
	}
	if err := t.checkLine(path, after, size); err != nil {
		return nil, err
	}
	p, err := t.primed(newParser, path, after)
	if err != nil {
		return nil, err
	}
	return &TranscriptTail{t: t, path: path, newParser: newParser, parser: p, offset: after}, nil
}

func (tt *TranscriptTail) Read() ([]domain.Message, error) {
	size, _, err := tt.t.size(tt.path)
	if err != nil || size <= tt.offset {
		return nil, err
	}
	data, err := tt.t.Files.ReadAt(tt.path, tt.offset, size-tt.offset)
	if err != nil {
		return nil, err
	}
	msgs, end := tt.parser.Parse(data, tt.offset)
	tt.offset = end
	return tt.calls.Resolve(msgs), nil
}

func (tt *TranscriptTail) Since(after int64) ([]domain.Message, error) {
	if after == tt.offset {
		return nil, nil
	}
	if err := tt.t.checkLine(tt.path, after, tt.offset); err != nil {
		return nil, err
	}
	p, err := tt.t.primed(tt.newParser, tt.path, after)
	if err != nil {
		return nil, err
	}
	data, err := tt.t.Files.ReadAt(tt.path, after, tt.offset-after)
	if err != nil {
		return nil, err
	}
	msgs, _ := p.Parse(data, after)
	var calls domain.ToolCalls
	out := calls.Resolve(msgs)
	tt.calls.Adopt(calls)
	return out, nil
}
