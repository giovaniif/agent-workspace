package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	CloseBadHandshake  websocket.StatusCode = 4400
	CloseUnauthorized  websocket.StatusCode = 4401
	streamReadLimit                         = 64 << 10
	streamWriteTimeout                      = 10 * time.Second
	maxWatches                              = 32
)

var (
	errClientGone = errors.New("client gone")
	errDaemonGone = errors.New("the agentws daemon went away")
)

type StreamState struct {
	Seq        uint64              `json:"seq"`
	Workspaces []domain.Workspace  `json:"workspaces"`
	Tasks      []domain.Task       `json:"tasks"`
	Worktrees  []domain.Worktree   `json:"worktrees"`
	Sessions   []StreamSession     `json:"sessions"`
	Limits     []StreamQuota       `json:"limits"`
	Queue      []domain.LaunchItem `json:"queue"`
	Sends      []domain.QueuedSend `json:"sends"`
}

type StreamDiff struct {
	Seq              uint64               `json:"seq"`
	RemovedWorkspace string               `json:"removed_workspace,omitempty"`
	RemovedWorktree  string               `json:"removed_worktree,omitempty"`
	RemovedSession   string               `json:"removed_session,omitempty"`
	Workspace        *domain.Workspace    `json:"workspace,omitempty"`
	Task             *domain.Task         `json:"task,omitempty"`
	Worktree         *domain.Worktree     `json:"worktree,omitempty"`
	Session          *StreamSession       `json:"session,omitempty"`
	Limits           *[]StreamQuota       `json:"limits,omitempty"`
	Queue            *[]domain.LaunchItem `json:"queue,omitempty"`
	Sends            *[]domain.QueuedSend `json:"sends,omitempty"`
}

type Frame struct {
	State      *StreamState     `json:"state,omitempty"`
	Diff       *StreamDiff      `json:"diff,omitempty"`
	Transcript *TranscriptFrame `json:"transcript,omitempty"`
	Error      *rpc.Error       `json:"error,omitempty"`
	Watch      string           `json:"watch,omitempty"`
}

type TranscriptFrame struct {
	Session  string        `json:"session"`
	Messages []rpc.Message `json:"messages"`
	Reset    bool          `json:"reset,omitempty"`
	Closed   bool          `json:"closed,omitempty"`
}

type clientFrame struct {
	Token   string  `json:"token"`
	Watch   *string `json:"watch"`
	After   int64   `json:"after"`
	Unwatch *string `json:"unwatch"`
}

type stream struct {
	conn    *websocket.Conn
	readCtx context.Context
	d       Daemon
	writeMu sync.Mutex
	watchMu sync.Mutex
	watches map[string]*watch
}

type watch struct {
	cancel context.CancelFunc
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	if err := s.checkOrigin(r); err != nil {
		writeError(w, err)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(streamReadLimit)
	ctx, cancel := context.WithCancelCause(s.base)
	defer cancel(nil)
	readCtx, stopReading := context.WithCancel(context.Background())
	defer stopReading()
	st := &stream{conn: conn, readCtx: readCtx}
	code, reason := s.runStream(ctx, cancel, st)
	_ = conn.Close(code, reason)
	if code != websocket.StatusNormalClosure && code != websocket.StatusGoingAway {
		s.log.printf("stream closed %d from %s: %s", code, clientAddr(r), reason)
	}
}

func (s *Server) checkOrigin(r *http.Request) error {
	if s.origin == "" {
		return &rpc.Error{Code: codeForbidden, Message: "agentws serve has no public URL; set [serve] url in config.toml or pass --url"}
	}
	origin, err := originOf(r.Header.Get("Origin"))
	if err != nil || origin != s.origin {
		return &rpc.Error{Code: codeForbidden, Message: "the stream only accepts the Origin " + s.origin}
	}
	return nil
}

func (s *Server) runStream(ctx context.Context, cancel context.CancelCauseFunc, st *stream) (websocket.StatusCode, string) {
	d, sub, device, code, reason := s.authenticate(ctx, st)
	if d == nil {
		return code, reason
	}
	defer func() { _ = d.Close() }()
	st.d = d
	defer st.unwatchAll()
	unregister, ok := s.streams.add(device.ID, cancel)
	if !ok {
		return CloseUnauthorized, errRevoked.Error()
	}
	defer unregister()
	v, state := newView(sub.State)
	diffs := s.relay(ctx, v, sub.Diffs)
	if err := st.write(ctx, Frame{State: state}); err != nil {
		return writeFailed(ctx)
	}
	go func() {
		cancel(st.readLoop(st.readCtx, ctx))
	}()
	for {
		select {
		case <-ctx.Done():
			return closeFor(context.Cause(ctx))
		case out, ok := <-diffs:
			if !ok {
				return websocket.StatusTryAgainLater, errDaemonGone.Error()
			}
			if err := st.write(ctx, Frame{Diff: out}); err != nil {
				return writeFailed(ctx)
			}
		}
	}
}

func writeFailed(ctx context.Context) (websocket.StatusCode, string) {
	if ctx.Err() != nil {
		return closeFor(context.Cause(ctx))
	}
	return websocket.StatusInternalError, "write failed"
}

func (s *Server) relay(ctx context.Context, v *view, in <-chan rpc.Diff) <-chan *StreamDiff {
	out := make(chan *StreamDiff)
	go func() {
		defer close(out)
		var queue []*StreamDiff
		for {
			var send chan<- *StreamDiff
			var head *StreamDiff
			if len(queue) > 0 {
				send, head = out, queue[0]
			}
			select {
			case <-ctx.Done():
				return
			case diff, ok := <-in:
				if !ok {
					return
				}
				if diff.RevokedDevice != "" {
					s.streams.revoke(diff.RevokedDevice)
				}
				queue = append(queue, v.apply(diff)...)
			case send <- head:
				queue = queue[1:]
			}
		}
	}()
	return out
}

func (s *Server) authenticate(ctx context.Context, st *stream) (Daemon, rpc.Subscription, rpc.Device, websocket.StatusCode, string) {
	var none rpc.Subscription
	stopping := context.AfterFunc(ctx, func() {
		_ = st.conn.Close(closeFor(context.Cause(ctx)))
	})
	lateReason := "no token within " + s.cfg.AuthTimeout.String()
	late := time.AfterFunc(s.cfg.AuthTimeout, func() {
		_ = st.conn.Close(CloseBadHandshake, lateReason)
	})
	_, data, err := st.conn.Read(st.readCtx)
	serving := stopping()
	if !late.Stop() {
		return nil, none, rpc.Device{}, CloseBadHandshake, lateReason
	}
	if !serving {
		code, reason := closeFor(context.Cause(ctx))
		return nil, none, rpc.Device{}, code, reason
	}
	if err != nil {
		return nil, none, rpc.Device{}, websocket.StatusNormalClosure, errClientGone.Error()
	}
	var first clientFrame
	if json.Unmarshal(data, &first) != nil || first.Token == "" {
		return nil, none, rpc.Device{}, CloseBadHandshake, `the first frame must be {"token": "<device token>"}`
	}
	d, err := s.dial(ctx)
	if err != nil {
		return nil, none, rpc.Device{}, websocket.StatusTryAgainLater, apiError(err).Message
	}
	sub, err := d.Subscribe(ctx)
	if err != nil {
		_ = d.Close()
		return nil, none, rpc.Device{}, websocket.StatusTryAgainLater, apiError(err).Message
	}
	device, err := s.check(ctx, d, first.Token)
	if err != nil {
		_ = d.Close()
		e := apiError(err)
		if e.Code == rpc.CodeUnauthorized {
			return nil, none, rpc.Device{}, CloseUnauthorized, e.Message
		}
		return nil, none, rpc.Device{}, websocket.StatusTryAgainLater, e.Message
	}
	return d, sub, device, 0, ""
}

func (st *stream) readLoop(readCtx, ctx context.Context) error {
	for {
		_, data, err := st.conn.Read(readCtx)
		if err != nil {
			return errClientGone
		}
		if err := st.handle(ctx, data); err != nil {
			return err
		}
	}
}

func (st *stream) handle(ctx context.Context, data []byte) error {
	var f clientFrame
	if json.Unmarshal(data, &f) != nil {
		return st.write(ctx, Frame{Error: &rpc.Error{Code: rpc.CodeBadRequest, Message: "frames are JSON objects"}})
	}
	switch {
	case f.Watch != nil && *f.Watch != "":
		return st.watch(ctx, *f.Watch, f.After)
	case f.Unwatch != nil && *f.Unwatch != "":
		st.unwatch(*f.Unwatch, nil)
		return nil
	}
	return st.write(ctx, Frame{Error: &rpc.Error{Code: rpc.CodeBadRequest, Message: `send {"watch": "<session>", "after": <cursor>} or {"unwatch": "<session>"}`}})
}

func (st *stream) watch(ctx context.Context, session string, after int64) error {
	st.watchMu.Lock()
	if old := st.watches[session]; old == nil && len(st.watches) >= maxWatches {
		st.watchMu.Unlock()
		return st.write(ctx, Frame{Watch: session, Error: &rpc.Error{Code: rpc.CodeBadRequest, Message: "too many watched sessions on one stream"}})
	} else if old != nil {
		old.cancel()
	}
	wctx, cancel := context.WithCancel(ctx)
	w := &watch{cancel: cancel}
	if st.watches == nil {
		st.watches = map[string]*watch{}
	}
	st.watches[session] = w
	st.watchMu.Unlock()
	go st.forward(wctx, w, session, after)
	return nil
}

func (st *stream) forward(ctx context.Context, w *watch, session string, after int64) {
	defer st.unwatch(session, w)
	tw, err := st.d.WatchTranscript(ctx, session, after)
	if err != nil {
		if ctx.Err() == nil {
			_ = st.write(ctx, Frame{Watch: session, Error: apiError(err)})
		}
		return
	}
	if st.write(ctx, Frame{Transcript: &TranscriptFrame{Session: session, Messages: orEmpty(tw.Messages)}}) != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-tw.Events:
			if !ok {
				return
			}
			out := &TranscriptFrame{Session: session, Messages: orEmpty(ev.Messages), Reset: ev.Reset, Closed: ev.Closed}
			if st.write(ctx, Frame{Transcript: out}) != nil || ev.Closed {
				return
			}
		}
	}
}

func (st *stream) unwatch(session string, only *watch) {
	st.watchMu.Lock()
	defer st.watchMu.Unlock()
	w := st.watches[session]
	if w == nil || (only != nil && w != only) {
		return
	}
	w.cancel()
	delete(st.watches, session)
}

func (st *stream) unwatchAll() {
	st.watchMu.Lock()
	defer st.watchMu.Unlock()
	for session, w := range st.watches {
		w.cancel()
		delete(st.watches, session)
	}
}

func (st *stream) write(ctx context.Context, f Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	st.writeMu.Lock()
	defer st.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, streamWriteTimeout)
	defer cancel()
	return st.conn.Write(wctx, websocket.MessageText, data)
}

func closeFor(cause error) (websocket.StatusCode, string) {
	switch {
	case errors.Is(cause, errRevoked):
		return CloseUnauthorized, errRevoked.Error()
	case errors.Is(cause, errClientGone):
		return websocket.StatusNormalClosure, ""
	case errors.Is(cause, context.Canceled):
		return websocket.StatusGoingAway, "agentws serve is stopping"
	default:
		return websocket.StatusInternalError, cause.Error()
	}
}

func withoutPorts(wt domain.Worktree) domain.Worktree {
	wt.Ports = nil
	return wt
}

func orEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
