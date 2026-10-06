package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

type call struct {
	Method string
	Params json.RawMessage
	conn   *fakeConn
}

type fakeDaemon struct {
	mu      sync.Mutex
	down    bool
	devices map[string]rpc.Device
	results map[string]any
	errs    map[string]*rpc.Error
	state   rpc.State
	calls   []call
	subs    map[*fakeConn][]chan rpc.Diff
	dials   int
	closes  int
	watches []*fakeWatch
	initial map[string][]rpc.Message
}

type fakeWatch struct {
	session string
	after   int64
	events  chan rpc.TranscriptEvent
	ctx     context.Context
}

func newFakeDaemon() *fakeDaemon {
	return &fakeDaemon{
		devices: map[string]rpc.Device{},
		results: map[string]any{},
		errs:    map[string]*rpc.Error{},
		subs:    map[*fakeConn][]chan rpc.Diff{},
		initial: map[string][]rpc.Message{},
	}
}

func (f *fakeDaemon) dial(context.Context) (serve.Daemon, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return nil, errors.New("connect: no such file or directory")
	}
	f.dials++
	return &fakeConn{f: f}, nil
}

func (f *fakeDaemon) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Method)
	}
	return out
}

func (f *fakeDaemon) paramsOf(method string) []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []json.RawMessage
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c.Params)
		}
	}
	return out
}

func (f *fakeDaemon) connOf(method string) []*fakeConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeConn
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c.conn)
		}
	}
	return out
}

func (f *fakeDaemon) subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, chans := range f.subs {
		n += len(chans)
	}
	return n
}

func (f *fakeDaemon) broadcast(d rpc.Diff) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, chans := range f.subs {
		for _, ch := range chans {
			ch <- d
		}
	}
}

type fakeConn struct {
	f      *fakeDaemon
	closed bool
}

func (c *fakeConn) Call(_ context.Context, method string, params, out any) error {
	f := c.f
	raw, _ := json.Marshal(params)
	f.mu.Lock()
	f.calls = append(f.calls, call{Method: method, Params: raw, conn: c})
	var result any
	var rerr *rpc.Error
	if method == rpc.MethodDeviceCheck {
		var p rpc.DeviceCheckParams
		_ = json.Unmarshal(raw, &p)
		if dev, ok := f.devices[p.Token]; ok {
			result = rpc.DeviceChecked{Device: dev}
		} else {
			rerr = &rpc.Error{Code: rpc.CodeUnauthorized, Message: "unknown or revoked device token"}
		}
	} else {
		result, rerr = f.results[method], f.errs[method]
	}
	f.mu.Unlock()
	if rerr != nil {
		return rerr
	}
	if out == nil || result == nil {
		return nil
	}
	b, _ := json.Marshal(result)
	return json.Unmarshal(b, out)
}

func (c *fakeConn) Subscribe(context.Context) (rpc.Subscription, error) {
	f := c.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{Method: rpc.MethodSubscribe})
	ch := make(chan rpc.Diff, 16)
	f.subs[c] = append(f.subs[c], ch)
	return rpc.Subscription{State: f.state, Diffs: ch}, nil
}

func (c *fakeConn) Close() error {
	f := c.f
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	f.closes++
	for _, ch := range f.subs[c] {
		close(ch)
	}
	delete(f.subs, c)
	return nil
}

func (c *fakeConn) WatchTranscript(ctx context.Context, session string, after int64) (rpc.TranscriptWatch, error) {
	f := c.f
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(rpc.TranscriptWatchParams{Session: session, After: after})
	f.calls = append(f.calls, call{Method: rpc.MethodTranscriptWatch, Params: raw})
	if rerr := f.errs[rpc.MethodTranscriptWatch]; rerr != nil {
		return rpc.TranscriptWatch{}, rerr
	}
	w := &fakeWatch{session: session, after: after, events: make(chan rpc.TranscriptEvent, 16), ctx: ctx}
	f.watches = append(f.watches, w)
	return rpc.TranscriptWatch{Messages: f.initial[session], Events: w.events}, nil
}

func (f *fakeDaemon) watchesOf(session string) []*fakeWatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeWatch
	for _, w := range f.watches {
		if w.session == session {
			out = append(out, w)
		}
	}
	return out
}
