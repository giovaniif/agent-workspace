package daemon

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const (
	browserTimeout    = 30 * time.Second
	browserMaxTimeout = 10 * time.Minute
)

type browserReply struct {
	result json.RawMessage
	err    string
	gone   bool
}

type browserWait struct {
	conn *conn
	ch   chan browserReply
}

type browserHub struct {
	mu      sync.Mutex
	conn    *conn
	attach  uint64
	next    uint64
	pending map[uint64]browserWait
}

func (h *browserHub) attachTo(c *conn, id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil && h.conn != c {
		h.failLocked(h.conn)
	}
	h.conn, h.attach = c, id
}

func (h *browserHub) drop(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == c {
		h.conn = nil
	}
	h.failLocked(c)
}

func (h *browserHub) failLocked(c *conn) {
	for id, w := range h.pending {
		if w.conn == c {
			w.ch <- browserReply{gone: true}
			delete(h.pending, id)
		}
	}
}

func (h *browserHub) send(session, command string, args json.RawMessage) (uint64, chan browserReply, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return 0, nil, false
	}
	if h.pending == nil {
		h.pending = map[uint64]browserWait{}
	}
	h.next++
	id := h.next
	ch := make(chan browserReply, 1)
	call := rpc.BrowserCall{Call: id, Session: session, Command: command, Args: args}
	if !h.conn.push(rpc.Response{V: rpc.Version, ID: h.attach, Browser: &call}) {
		h.conn = nil
		return 0, nil, false
	}
	h.pending[id] = browserWait{conn: h.conn, ch: ch}
	return id, ch, true
}

func (h *browserHub) connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

func (h *browserHub) forget(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pending, id)
}

func (h *browserHub) reply(c *conn, p rpc.BrowserReplyParams) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.pending[p.Call]
	if !ok || w.conn != c {
		return false
	}
	delete(h.pending, p.Call)
	w.ch <- browserReply{result: p.Result, err: p.Error}
	return true
}

func (d *Daemon) browserAttach(c *conn, req rpc.Request) (*rpc.Response, bool) {
	d.browser.attachTo(c, req.ID)
	return result(req.ID, struct {
		Attached bool `json:"attached"`
	}{true}), true
}

func (d *Daemon) browserReply(c *conn, req rpc.Request) (*rpc.Response, bool) {
	var p rpc.BrowserReplyParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.reply params: "+err.Error()), true
	}
	if !d.browser.reply(c, p) {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no browser call %d waits for this connection", p.Call)), true
	}
	return result(req.ID, struct{}{}), true
}

func (d *Daemon) browserCommand(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.BrowserCommandParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.command params: "+err.Error()), true
	}
	if !slices.Contains(domain.BrowserCommands(), p.Command) {
		return errorResponse(req.ID, rpc.CodeBadRequest, fmt.Sprintf("unknown browser command %q", p.Command)), true
	}
	session := p.Session
	if session == "" && p.Pane != "" {
		ok := d.query(func(s *state) {
			if found, on := domain.SessionOnPane(s.sessionList(), p.Pane); on {
				session = found.ID
			}
		})
		if !ok {
			return nil, false
		}
		if session == "" {
			return errorResponse(req.ID, rpc.CodeBadRequest, "no agentws session runs on pane "+p.Pane+": pass --session"), true
		}
	}
	if session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.command needs a session or a pane"), true
	}
	args, err := json.Marshal(p.Args)
	if err != nil || p.Args == nil {
		args = json.RawMessage(`{}`)
	}
	timeout := browserTimeout
	if p.TimeoutMS > 0 {
		timeout = min(time.Duration(p.TimeoutMS)*time.Millisecond, browserMaxTimeout)
	}
	cfg := d.headless.config()
	if d.headless.opener == nil {
		cfg.Fallback = domain.BrowserFallbackOff
	}
	backend, ok := domain.ChooseBrowser(d.headless.want(session), d.browser.connected(), cfg)
	if !ok {
		return errorResponse(req.ID, rpc.CodeUnavailable, rpc.NoBrowser), true
	}
	if backend == domain.BrowserHeadless {
		return d.headlessCommand(req, session, p.Command, args, timeout), true
	}
	id, ch, ok := d.browser.send(session, p.Command, args)
	if !ok {
		return errorResponse(req.ID, rpc.CodeUnavailable, rpc.NoBrowser), true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		switch {
		case r.gone:
			return errorResponse(req.ID, rpc.CodeUnavailable, "the macOS app disconnected before it answered"), true
		case r.err != "":
			return errorResponse(req.ID, rpc.CodeFailed, r.err), true
		case len(r.result) == 0:
			return result(req.ID, struct{}{}), true
		}
		return &rpc.Response{V: rpc.Version, ID: req.ID, Result: r.result}, true
	case <-timer.C:
		d.browser.forget(id)
		return errorResponse(req.ID, rpc.CodeFailed, fmt.Sprintf("browser: no reply from the macOS app in %v", timeout)), true
	}
}
