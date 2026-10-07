package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/version"
)

func checkBuild(method string, resp Response) error {
	if resp.Build == "" && resp.Error == nil && !AnyBuild(method) {
		return Mismatch("", version.String(), 0, builtAtUnix())
	}
	return nil
}

func builtAtUnix() int64 {
	t := version.BuiltAt()
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

const StartTimeout = 2 * time.Second

const retryInterval = 25 * time.Millisecond

var ErrClosed = errors.New("rpc: connection closed")

type Client struct {
	conn net.Conn

	writeMu sync.Mutex
	enc     *json.Encoder

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan Response
	closed  bool
}

func Dial(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		pending: map[uint64]chan Response{},
	}
	go c.read()
	return c, nil
}

func Connect(ctx context.Context, path string, start func() error) (*Client, error) {
	if c, err := Dial(path); err == nil {
		return c, nil
	}
	if err := start(); err != nil {
		return nil, fmt.Errorf("rpc: start daemon: %w", err)
	}
	deadline := time.Now().Add(StartTimeout)
	for {
		c, err := Dial(path)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("rpc: daemon did not start within %v: %w", StartTimeout, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	id, ch, err := c.send(method, params)
	if err != nil {
		return err
	}
	defer c.forget(id)
	resp, err := c.await(ctx, ch)
	if err != nil {
		return err
	}
	if err := checkBuild(method, resp); err != nil {
		return err
	}
	return decode(resp, out)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	err := c.Call(ctx, MethodStatus, nil, &st)
	return st, err
}

func (c *Client) WorkspaceAdd(ctx context.Context, path string) (domain.Workspace, error) {
	var ws domain.Workspace
	err := c.Call(ctx, MethodWorkspaceAdd, WorkspaceAddParams{Path: path}, &ws)
	return ws, err
}

func (c *Client) WorkspaceDirs(ctx context.Context, path string) (WorkspaceDirs, error) {
	var dirs WorkspaceDirs
	err := c.Call(ctx, MethodWorkspaceDirs, WorkspaceDirsParams{Path: path}, &dirs)
	return dirs, err
}

func (c *Client) WorkspaceList(ctx context.Context) (WorkspaceList, error) {
	var list WorkspaceList
	err := c.Call(ctx, MethodWorkspaceList, nil, &list)
	return list, err
}

func (c *Client) WorktreeAssign(ctx context.Context, id, session string) error {
	return c.Call(ctx, MethodWorktreeAssign, WorktreeAssignParams{ID: id, Session: session}, nil)
}

func (c *Client) KillPorts(ctx context.Context, pgids []int) ([]int, error) {
	var out PortsKilled
	err := c.Call(ctx, MethodPortsKill, PortsKillParams{PGIDs: pgids}, &out)
	return out.Killed, err
}

func (c *Client) CleanupPlan(ctx context.Context) ([]CleanupItem, error) {
	var items []CleanupItem
	err := c.Call(ctx, MethodCleanupPlan, nil, &items)
	return items, err
}

func (c *Client) CleanupRun(ctx context.Context) ([]CleanupItem, error) {
	var items []CleanupItem
	err := c.Call(ctx, MethodCleanupRun, nil, &items)
	return items, err
}

func (c *Client) DiskView(ctx context.Context) (DiskView, error) {
	var v DiskView
	err := c.Call(ctx, MethodDiskView, nil, &v)
	return v, err
}

func (c *Client) CleanupWorktree(ctx context.Context, path string, backup bool) (CleanupItem, error) {
	var item CleanupItem
	err := c.Call(ctx, MethodCleanupWorktree, CleanupWorktreeParams{Path: path, Backup: backup}, &item)
	return item, err
}

func (c *Client) WorktreeShell(ctx context.Context, id string) error {
	return c.Call(ctx, MethodShellToggle, ShellParams{Worktree: id}, nil)
}

func (c *Client) ProjectAdd(ctx context.Context, p ProjectAddParams) (domain.Project, error) {
	var out domain.Project
	err := c.Call(ctx, MethodProjectAdd, p, &out)
	return out, err
}

func (c *Client) ProjectList(ctx context.Context) (ProjectList, error) {
	var out ProjectList
	err := c.Call(ctx, MethodProjectList, nil, &out)
	return out, err
}

func (c *Client) ProjectRemove(ctx context.Context, root string) error {
	return c.Call(ctx, MethodProjectRemove, ProjectRemoveParams{Root: root}, nil)
}

func (c *Client) WorkspaceRemove(ctx context.Context, root string) error {
	return c.Call(ctx, MethodWorkspaceRemove, WorkspaceRemoveParams{Root: root}, nil)
}

func (c *Client) OpenClient(ctx context.Context, p OpenClientParams) (OpenClient, error) {
	var out OpenClient
	err := c.Call(ctx, MethodOpenClient, p, &out)
	return out, err
}

func (c *Client) NativeClient(ctx context.Context, p NativeClientParams) (NativeClient, error) {
	var out NativeClient
	err := c.Call(ctx, MethodClientNative, p, &out)
	return out, err
}

func (c *Client) FocusMain(ctx context.Context) error {
	return c.Call(ctx, MethodFocusMain, nil, nil)
}

func (c *Client) MuteSession(ctx context.Context, id string, muted bool) error {
	return c.Call(ctx, MethodSessionMute, SessionMuteParams{ID: id, Muted: muted}, nil)
}

func (c *Client) FocusSession(ctx context.Context, id string) error {
	return c.Call(ctx, MethodSessionFocus, SessionFocusParams{ID: id}, nil)
}

func (c *Client) Review(ctx context.Context, p ReviewParams) (Review, error) {
	var out Review
	err := c.Call(ctx, MethodReviewOpen, p, &out)
	return out, err
}

func (c *Client) MarkViewed(ctx context.Context, mark domain.ViewedMark, viewed bool) error {
	return c.Call(ctx, MethodReviewViewed, ViewedParams{Mark: mark, Viewed: viewed}, nil)
}

func (c *Client) AddReviewComment(ctx context.Context, p CommentParams) (domain.ReviewDraft, error) {
	var out domain.ReviewDraft
	err := c.Call(ctx, MethodReviewComment, p, &out)
	return out, err
}

func (c *Client) SendReview(ctx context.Context, session string) (domain.ReviewDraft, error) {
	var out domain.ReviewDraft
	err := c.Call(ctx, MethodReviewSend, ReviewSendParams{Session: session}, &out)
	return out, err
}

func (c *Client) SessionSend(ctx context.Context, session, text string) (SessionSent, error) {
	var out SessionSent
	err := c.Call(ctx, MethodSessionSend, SessionSendParams{Session: session, Text: text}, &out)
	return out, err
}

func (c *Client) SessionUnsend(ctx context.Context, session, id string) error {
	return c.Call(ctx, MethodSessionUnsend, SessionUnsendParams{Session: session, ID: id}, nil)
}

func (c *Client) SessionInterrupt(ctx context.Context, session string) error {
	return c.Call(ctx, MethodSessionInterrupt, SessionTarget{Session: session}, nil)
}

func (c *Client) ApplyHunk(ctx context.Context, p HunkParams) error {
	return c.Call(ctx, MethodReviewHunk, p, nil)
}

func (c *Client) ReviewLayout(ctx context.Context, open bool) error {
	return c.Call(ctx, MethodClientReview, ClientReviewParams{Open: open}, nil)
}

func (c *Client) DebugSeed(ctx context.Context, p DebugSeedParams) error {
	return c.Call(ctx, MethodDebugSeed, p, nil)
}

type Subscription struct {
	State State
	Diffs <-chan Diff
}

func (c *Client) Subscribe(ctx context.Context) (Subscription, error) {
	id, ch, err := c.send(MethodSubscribe, nil)
	if err != nil {
		return Subscription{}, err
	}
	resp, err := c.await(ctx, ch)
	if err == nil {
		err = checkBuild(MethodSubscribe, resp)
	}
	if err == nil {
		err = decode(resp, nil)
	}
	var sub Subscription
	if err == nil {
		err = json.Unmarshal(resp.Result, &sub.State)
	}
	if err != nil {
		c.forget(id)
		return Subscription{}, err
	}
	diffs := make(chan Diff, 64)
	go func() {
		defer close(diffs)
		for resp := range ch {
			if resp.Diff != nil {
				diffs <- *resp.Diff
			}
		}
	}()
	sub.Diffs = diffs
	return sub, nil
}

func (c *Client) StreamNotices(ctx context.Context) (<-chan Notice, error) {
	id, ch, err := c.send(MethodNotifyStream, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.await(ctx, ch)
	if err == nil {
		err = checkBuild(MethodNotifyStream, resp)
	}
	if err == nil {
		err = decode(resp, nil)
	}
	if err != nil {
		c.forget(id)
		return nil, err
	}
	notices := make(chan Notice, 64)
	go func() {
		defer close(notices)
		for {
			var resp Response
			var ok bool
			select {
			case <-ctx.Done():
				c.forget(id)
				return
			case resp, ok = <-ch:
			}
			if !ok {
				return
			}
			if resp.Notice == nil {
				continue
			}
			select {
			case notices <- *resp.Notice:
			case <-ctx.Done():
				c.forget(id)
				return
			}
		}
	}()
	return notices, nil
}

func (c *Client) send(method string, params any) (uint64, chan Response, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return 0, nil, err
		}
		raw = b
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan Response, 16)
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err := c.enc.Encode(Request{V: Version, ID: id, Method: method, Params: raw, Build: version.String(), BuiltAt: builtAtUnix()})
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return 0, nil, err
	}
	return id, ch, nil
}

func (c *Client) await(ctx context.Context, ch chan Response) (Response, error) {
	select {
	case resp, ok := <-ch:
		if !ok {
			return Response{}, ErrClosed
		}
		return resp, nil
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
}

func (c *Client) forget(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.pending[id]; ok {
		delete(c.pending, id)
		close(ch)
	}
}

func (c *Client) read() {
	defer c.shutdown()
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 64*1024), MaxMessage)
	for sc.Scan() {
		var resp Response
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			return
		}
		c.mu.Lock()
		if ch := c.pending[resp.ID]; ch != nil {
			ch <- resp
		}
		c.mu.Unlock()
	}
}

func (c *Client) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}

func decode(resp Response, out any) error {
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

const MaxMessage = 16 << 20
