package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	transcriptChunk = domain.TranscriptPageMax
	transcriptPoll  = time.Second
)

type watchKey struct {
	c  *conn
	id uint64
}

type transcripts struct {
	tr      app.Transcripts
	watcher app.TranscriptWatcher
	on      bool
	ctx     context.Context
	mu      sync.Mutex
	tailers map[string]*tailer
	byKey   map[watchKey]string
}

func WithTranscripts(tr app.Transcripts, watcher app.TranscriptWatcher) Option {
	return func(d *Daemon) {
		d.tx = &transcripts{tr: tr, watcher: watcher, on: true, ctx: context.Background(), tailers: map[string]*tailer{}, byKey: map[watchKey]string{}}
		d.st.transcriptMoved = d.tx.moved
	}
}

func (d *Daemon) transcriptMethod(c *conn, req rpc.Request) (*rpc.Response, bool) {
	if d.tx == nil {
		return errorResponse(req.ID, rpc.CodeUnknownMethod, "unknown method "+req.Method), true
	}
	switch req.Method {
	case rpc.MethodTranscriptPage:
		return d.transcriptPage(req)
	case rpc.MethodTranscriptWatch:
		return d.transcriptWatch(c, req)
	default:
		var p rpc.TranscriptUnwatchParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "unwatch params: "+err.Error()), true
		}
		d.tx.leave(watchKey{c: c, id: p.Watch})
		return result(req.ID, struct{}{}), true
	}
}

func transcriptError(id uint64, err error) *rpc.Response {
	switch {
	case errors.Is(err, app.ErrBadCursor):
		return errorResponse(id, rpc.CodeBadRequest, err.Error())
	case errors.Is(err, app.ErrNoTranscriptParser):
		return errorResponse(id, rpc.CodeUnavailable, err.Error())
	default:
		return errorResponse(id, rpc.CodeFailed, err.Error())
	}
}

func (d *Daemon) transcriptPage(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.TranscriptPageParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "transcript.page params: "+err.Error()), true
	}
	var session domain.Session
	var found bool
	if !d.query(func(s *state) { session, found = s.sessions[p.Session] }) {
		return nil, false
	}
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session), true
	}
	if session.Transcript == "" {
		return result(req.ID, rpc.TranscriptPage{Messages: []rpc.Message{}}), true
	}
	page, err := d.tx.tr.Page(session.Harness, session.Transcript, p.Before, p.Limit)
	if err != nil {
		return transcriptError(req.ID, err), true
	}
	return result(req.ID, rpc.TranscriptPage{Messages: rpc.MessagesOf(page.Messages), Before: page.Before}), true
}

func (d *Daemon) transcriptWatch(c *conn, req rpc.Request) (*rpc.Response, bool) {
	var p rpc.TranscriptWatchParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "transcript.watch params: "+err.Error()), true
	}
	var found bool
	ok := d.query(func(s *state) {
		var session domain.Session
		if session, found = s.sessions[p.Session]; found {
			d.tx.join(watchKey{c: c, id: req.ID}, session, p.After)
		}
	})
	if !ok {
		return nil, false
	}
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no session "+p.Session), true
	}
	return nil, true
}

func (x *transcripts) join(key watchKey, session domain.Session, after int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	t := x.tailers[session.ID]
	if t == nil {
		ctx, cancel := context.WithCancel(x.ctx)
		t = &tailer{x: x, harness: session.Harness, keys: map[watchKey]bool{}, kick: make(chan struct{}, 1), cancel: cancel}
		x.tailers[session.ID] = t
		go t.run(ctx, session.Transcript)
	}
	t.keys[key] = true
	x.byKey[key] = session.ID
	t.add(tailCmd{join: &key, after: after})
}

func (x *transcripts) leave(key watchKey) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.leaveLocked(key)
}

func (x *transcripts) leaveLocked(key watchKey) {
	id, ok := x.byKey[key]
	if !ok {
		return
	}
	delete(x.byKey, key)
	t := x.tailers[id]
	delete(t.keys, key)
	if len(t.keys) == 0 {
		delete(x.tailers, id)
		t.cancel()
		return
	}
	t.add(tailCmd{leave: &key})
}

func (x *transcripts) dropConn(c *conn) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for key := range x.byKey {
		if key.c == c {
			x.leaveLocked(key)
		}
	}
}

func (x *transcripts) moved(sessionID, path string, gone bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	t := x.tailers[sessionID]
	if t == nil {
		return
	}
	if !gone {
		t.add(tailCmd{path: &path})
		return
	}
	for key := range t.keys {
		delete(x.byKey, key)
	}
	delete(x.tailers, sessionID)
	t.add(tailCmd{closed: true})
}

type tailCmd struct {
	join   *watchKey
	after  int64
	leave  *watchKey
	path   *string
	closed bool
}

type tailer struct {
	x       *transcripts
	harness domain.Harness
	keys    map[watchKey]bool
	kick    chan struct{}
	cancel  context.CancelFunc
	mu      sync.Mutex
	cmds    []tailCmd
}

func (t *tailer) add(cmd tailCmd) {
	t.mu.Lock()
	t.cmds = append(t.cmds, cmd)
	t.mu.Unlock()
	select {
	case t.kick <- struct{}{}:
	default:
	}
}

func (t *tailer) take() []tailCmd {
	t.mu.Lock()
	defer t.mu.Unlock()
	cmds := t.cmds
	t.cmds = nil
	return cmds
}

type tailRun struct {
	t        *tailer
	ctx      context.Context
	path     string
	tail     *app.TranscriptTail
	changes  <-chan struct{}
	poll     *time.Ticker
	stop     context.CancelFunc
	watchers map[watchKey]bool
}

func (t *tailer) run(ctx context.Context, path string) {
	r := &tailRun{t: t, ctx: ctx, path: path, stop: func() {}, watchers: map[watchKey]bool{}}
	defer t.cancel()
	defer r.close()
	for {
		var tick <-chan time.Time
		if r.poll != nil {
			tick = r.poll.C
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-r.changes:
			if !ok {
				r.changes = nil
			}
			r.broadcast(rpc.TranscriptEvent{}, r.read())
		case <-tick:
			r.broadcast(rpc.TranscriptEvent{}, r.read())
		case <-t.kick:
			if !r.apply(t.take()) {
				return
			}
		}
	}
}

func (r *tailRun) apply(cmds []tailCmd) bool {
	for _, cmd := range cmds {
		switch {
		case cmd.join != nil:
			r.join(*cmd.join, cmd.after)
		case cmd.leave != nil:
			delete(r.watchers, *cmd.leave)
		case cmd.path != nil:
			r.path = *cmd.path
			r.close()
			var msgs []domain.Message
			if err := r.open(0); err != nil {
				log.Printf("[transcript] %s: %v", r.path, err)
			} else {
				msgs = r.read()
			}
			r.broadcastAll(rpc.TranscriptEvent{Reset: true}, msgs)
		case cmd.closed:
			r.broadcastAll(rpc.TranscriptEvent{Closed: true}, nil)
			return false
		}
	}
	return true
}

func (r *tailRun) join(key watchKey, after int64) {
	var msgs []domain.Message
	var err error
	switch {
	case r.path == "":
	case r.tail == nil:
		if err = r.open(after); err == nil {
			msgs = r.read()
		}
	default:
		r.broadcast(rpc.TranscriptEvent{}, r.read())
		msgs, err = r.tail.Since(after)
	}
	if err != nil {
		key.c.push(*transcriptError(key.id, err))
		r.t.x.leave(key)
		return
	}
	first := msgs[:min(len(msgs), transcriptChunk)]
	if !key.c.push(*result(key.id, rpc.TranscriptWatched{Messages: rpc.MessagesOf(first)})) {
		return
	}
	r.watchers[key] = true
	for rest := msgs[len(first):]; len(rest) > 0; {
		n := min(len(rest), transcriptChunk)
		key.c.push(rpc.Response{V: rpc.Version, ID: key.id, Transcript: &rpc.TranscriptEvent{Messages: rpc.MessagesOf(rest[:n])}})
		rest = rest[n:]
	}
}

func (r *tailRun) open(after int64) error {
	r.close()
	if r.path == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.stop = cancel
	changes, err := r.t.x.watcher.Watch(ctx, r.path)
	if err != nil {
		r.poll = time.NewTicker(transcriptPoll)
	} else {
		r.changes = changes
	}
	tail, err := r.t.x.tr.Tail(r.t.harness, r.path, after)
	if err != nil {
		r.close()
		return err
	}
	r.tail = tail
	return nil
}

func (r *tailRun) close() {
	r.stop()
	r.stop = func() {}
	if r.poll != nil {
		r.poll.Stop()
		r.poll = nil
	}
	r.changes = nil
	r.tail = nil
}

func (r *tailRun) read() []domain.Message {
	if r.tail == nil {
		return nil
	}
	msgs, err := r.tail.Read()
	if err != nil {
		log.Printf("[transcript] %s: %v", r.path, err)
	}
	return msgs
}

func (r *tailRun) broadcast(head rpc.TranscriptEvent, msgs []domain.Message) {
	if len(msgs) == 0 {
		return
	}
	r.broadcastAll(head, msgs)
}

func (r *tailRun) broadcastAll(head rpc.TranscriptEvent, msgs []domain.Message) {
	for {
		n := min(len(msgs), transcriptChunk)
		ev := head
		ev.Messages = rpc.MessagesOf(msgs[:n])
		if n == 0 {
			ev.Messages = nil
		}
		for key := range r.watchers {
			key.c.push(rpc.Response{V: rpc.Version, ID: key.id, Transcript: &ev})
		}
		msgs = msgs[n:]
		head = rpc.TranscriptEvent{}
		if len(msgs) == 0 {
			return
		}
	}
}
