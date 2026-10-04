package app_test

import (
	"io/fs"
	"strings"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type lineParser struct {
	log domain.MessageLog
}

func (p *lineParser) Parse(data []byte, base int64) ([]domain.Message, int64) {
	lines, end := domain.TranscriptLines(data, base)
	for _, l := range lines {
		f := strings.Fields(string(l.Data))
		if len(f) < 2 {
			continue
		}
		m := domain.Message{ID: f[1], Cursor: l.End, Text: strings.Join(f[2:], " ")}
		switch f[0] {
		case "u":
			m.Role = domain.RoleUser
			p.log.Add(m)
		case "a":
			m.Role = domain.RoleAssistant
			p.log.Add(m)
		case "a2":
			m.Role = domain.RoleAssistant
			p.log.Add(m)
			m.ID += ":1"
			p.log.Add(m)
		case "call":
			m.Role = domain.RoleTool
			m.Tool = &domain.MessageTool{Name: "Bash", Summary: "Bash " + m.Text, Status: domain.ToolRunning}
			m.Text = ""
			p.log.Add(m)
		case "ok":
			r := domain.ToolResult(f[1], domain.ToolDone, m.Text)
			r.Cursor = l.End
			p.log.Finish(r)
		}
	}
	return p.log.Drain(), end
}

func lineParsers() map[domain.Harness]func() app.TranscriptParser {
	return map[domain.Harness]func() app.TranscriptParser{
		domain.HarnessClaude: func() app.TranscriptParser { return &lineParser{} },
	}
}

type memFiles struct {
	mu    sync.Mutex
	files map[string][]byte
	reads int
}

func newMemFiles(path, content string) *memFiles {
	return &memFiles{files: map[string][]byte{path: []byte(content)}}
}

func (f *memFiles) append(path, s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = append(f.files[path], s...)
}

func (f *memFiles) Size(path string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[path]
	if !ok {
		return 0, fs.ErrNotExist
	}
	return int64(len(b)), nil
}

func (f *memFiles) ReadAt(path string, off, n int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	b, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	end := min(off+n, int64(len(b)))
	return append([]byte(nil), b[off:end]...), nil
}
