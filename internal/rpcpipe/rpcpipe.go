package rpcpipe

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/view"
)

const MethodViewSubscribe = "view.subscribe"

var ErrUnavailable = errors.New("rpcpipe: no daemon is listening")

type nativeResponse struct {
	V      int               `json:"v"`
	ID     uint64            `json:"id"`
	Result *view.NativeState `json:"result,omitempty"`
	Diff   *view.NativeDiff  `json:"diff,omitempty"`
	Build  string            `json:"build,omitempty"`
}

type views struct {
	mu     sync.Mutex
	byID   map[uint64]*view.View
	open   map[uint64]bool
	tokens map[uint64]bool
}

func Run(socket string, in io.Reader, out io.Writer) error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		line, _ := json.Marshal(rpc.Response{V: rpc.Version, Error: &rpc.Error{Code: rpc.CodeUnavailable, Message: "no agentws daemon is running: " + err.Error()}})
		_, _ = out.Write(append(line, '\n'))
		return ErrUnavailable
	}
	defer func() { _ = conn.Close() }()
	vs := &views{byID: map[uint64]*view.View{}, open: map[uint64]bool{}, tokens: map[uint64]bool{}}
	upErr := make(chan error, 1)
	go func() {
		err := copyLines(conn, in, vs.request)
		upErr <- err
		if err != nil {
			_ = conn.Close()
			return
		}
		if uc, ok := conn.(*net.UnixConn); ok {
			_ = uc.CloseWrite()
		}
	}()
	downErr := copyLines(out, conn, vs.response)
	select {
	case err := <-upErr:
		if err != nil {
			return err
		}
	default:
	}
	if errors.Is(downErr, net.ErrClosed) {
		return nil
	}
	return downErr
}

func (vs *views) request(line []byte) [][]byte {
	var req rpc.Request
	if json.Unmarshal(line, &req) != nil {
		return [][]byte{line}
	}
	if wantsTokens(req) {
		vs.mu.Lock()
		vs.tokens[req.ID] = true
		vs.mu.Unlock()
		return [][]byte{line}
	}
	if req.Method != MethodViewSubscribe {
		return [][]byte{line}
	}
	req.Method = rpc.MethodSubscribe
	req.Params = nil
	out, err := json.Marshal(req)
	if err != nil {
		return [][]byte{line}
	}
	vs.mu.Lock()
	vs.open[req.ID] = true
	delete(vs.byID, req.ID)
	vs.mu.Unlock()
	return [][]byte{out}
}

func (vs *views) response(line []byte) [][]byte {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if len(vs.open) == 0 && len(vs.tokens) == 0 {
		return [][]byte{line}
	}
	var resp rpc.Response
	if json.Unmarshal(line, &resp) != nil {
		return [][]byte{line}
	}
	if vs.tokens[resp.ID] {
		delete(vs.tokens, resp.ID)
		return tokenised(line, resp)
	}
	if !vs.open[resp.ID] {
		return [][]byte{line}
	}
	if resp.Error != nil {
		delete(vs.open, resp.ID)
		delete(vs.byID, resp.ID)
		return [][]byte{line}
	}
	v := vs.byID[resp.ID]
	switch {
	case v == nil && resp.Result != nil:
		var st rpc.State
		if json.Unmarshal(resp.Result, &st) != nil {
			return [][]byte{line}
		}
		v, state := view.NewNative(st)
		vs.byID[resp.ID] = v
		return encode(nativeResponse{V: resp.V, ID: resp.ID, Result: state, Build: resp.Build})
	case v != nil && resp.Diff != nil:
		var lines [][]byte
		for _, d := range v.ApplyNative(*resp.Diff) {
			lines = append(lines, encode(nativeResponse{V: resp.V, ID: resp.ID, Diff: d})...)
		}
		return lines
	}
	return [][]byte{line}
}

func tokenised(line []byte, resp rpc.Response) [][]byte {
	if resp.Error != nil || resp.Result == nil {
		return [][]byte{line}
	}
	result, err := withSpans(resp.Result)
	if err != nil {
		return [][]byte{line}
	}
	resp.Result = result
	out, err := json.Marshal(resp)
	if err != nil {
		return [][]byte{line}
	}
	return [][]byte{out}
}

func encode(r nativeResponse) [][]byte {
	b, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	return [][]byte{b}
}

func copyLines(dst io.Writer, src io.Reader, rewrite func([]byte) [][]byte) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), rpc.MaxMessage)
	for sc.Scan() {
		for _, line := range rewrite(sc.Bytes()) {
			if _, err := dst.Write(append(append([]byte(nil), line...), '\n')); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
