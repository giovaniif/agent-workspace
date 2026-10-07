package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type harnesses struct {
	host     app.TerminalHost
	adapters map[domain.Harness]app.HarnessAdapter
	defaults map[domain.Harness]StartDefaults
	sendMu   sync.Mutex
}

func WithHarnesses(host app.TerminalHost, adapters ...app.HarnessAdapter) Option {
	return func(d *Daemon) {
		d.hs.host = host
		d.hs.adapters = map[domain.Harness]app.HarnessAdapter{}
		for _, a := range adapters {
			d.hs.adapters[a.Harness()] = a
		}
	}
}

func hookEvent(h rpc.Hook) (domain.HarnessEventKind, bool) {
	if domain.Harness(h.Harness) == domain.HarnessClaude {
		return claude.Event(h.Event, h.Payload)
	}
	return domain.HookEvent(domain.Harness(h.Harness), h.Event)
}

func (s *state) statusLine(sl rpc.StatusLine) {
	session, ok := domain.SessionOnPane(s.sessionList(), sl.Pane)
	if !ok {
		return
	}
	if sl.Report.At.IsZero() {
		sl.Report.At = time.Now()
	}
	s.emit(SessionChanged{Session: session.Report(sl.Report)})
}

func (s *state) sessionList() []domain.Session {
	out := make([]domain.Session, 0, len(s.sessions))
	for _, x := range s.sessions {
		out = append(out, x)
	}
	return out
}

func (d *Daemon) launch(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.LaunchParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "launch params: "+err.Error()), true
	}
	adapter, ok := d.hs.adapters[domain.Harness(p.Harness)]
	if !ok || d.hs.host == nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "no harness "+p.Harness), true
	}
	spec := adapter.Launch(app.LaunchRequest{Name: p.Name, Dir: p.Dir, Model: p.Model, Effort: p.Effort, Prompt: p.Prompt})
	pane, err := d.hs.host.Create(context.Background(), spec)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeLaunchFailed, err.Error()), true
	}
	session := domain.Session{
		ID:      newID(),
		Harness: adapter.Harness(),
		Pane:    string(pane),
		Model:   p.Model,
		Effort:  p.Effort,
		State:   domain.StateIdle,
		Dir:     p.Dir,
	}
	if p.Name != "" {
		task := domain.Task{ID: newID(), Source: domain.TaskText, Text: p.Name}
		session.TaskID = task.ID
		if !d.commit(TaskChanged{Task: task}) {
			return nil, false
		}
	}
	if !d.commit(SessionChanged{Session: session}) {
		return nil, false
	}
	return result(req.ID, session), true
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
