package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/version"
)

func shortDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "crew-d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func start(t testing.TB, store app.Store, opts ...daemon.Option) (*daemon.Daemon, string) {
	t.Helper()
	d, err := daemon.New(store, 1234, opts...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(shortDir(t), "crew.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Serve(ctx, ln)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return d, path
}

func dial(t testing.TB, path string) *rpc.Client {
	t.Helper()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func next(t *testing.T, diffs <-chan rpc.Diff) rpc.Diff {
	t.Helper()
	select {
	case d, ok := <-diffs:
		if !ok {
			t.Fatal("diff stream closed")
		}
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("no diff")
	}
	return rpc.Diff{}
}

func TestSubscribersAllGetEveryDiffInTheSameOrder(t *testing.T) {
	d, path := start(t, &memStore{})
	ctx := context.Background()
	a, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const n = 50
	for i := range n {
		if i%2 == 0 {
			d.Post(daemon.SessionChanged{Session: domain.Session{ID: fmt.Sprint("s", i)}})
		} else {
			d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: fmt.Sprint("w", i)}})
		}
	}
	for i := range n {
		da, db := next(t, a.Diffs), next(t, b.Diffs)
		ja, _ := json.Marshal(da)
		jb, _ := json.Marshal(db)
		if string(ja) != string(jb) {
			t.Fatalf("diff %d differs: %+v vs %+v", i, da, db)
		}
		if da.Seq != a.State.Seq+uint64(i)+1 {
			t.Fatalf("diff %d has seq %d after state seq %d", i, da.Seq, a.State.Seq)
		}
		if (i%2 == 0) != (da.Session != nil) {
			t.Fatalf("diff %d out of order: %+v", i, da)
		}
	}
}

func TestSubscribeStartsFromTheCurrentState(t *testing.T) {
	d, path := start(t, &memStore{snap: app.Snapshot{Sessions: []domain.Session{{ID: "old"}}}})
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "new"}})
	ctx := context.Background()
	c := dial(t, path)
	deadline := time.Now().Add(2 * time.Second)
	for {
		sub, err := c.Subscribe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(sub.State.Sessions) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %+v", sub.State)
		}
	}
}

func TestStateChangesUpdateTheSameSession(t *testing.T) {
	d, path := start(t, &memStore{})
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", State: domain.StateRunning}})
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", State: domain.StateDone}})
	d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: "w"}})
	st := waitStatus(t, dial(t, path), func(s rpc.Status) bool { return s.Worktrees == 1 })
	if st.Sessions != 1 {
		t.Fatalf("status %+v", st)
	}
}

func TestChangesArePersisted(t *testing.T) {
	store := &memStore{}
	d, path := start(t, store)
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a"}})
	waitStatus(t, dial(t, path), func(s rpc.Status) bool { return s.Sessions == 1 })
	snap, _ := store.Load()
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != "a" {
		t.Fatalf("stored %+v", snap)
	}
}

func waitStatus(t *testing.T, c *rpc.Client, ok func(rpc.Status) bool) rpc.Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, err := c.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStatusReportsPIDAndRestoredCounts(t *testing.T) {
	store := &memStore{snap: app.Snapshot{
		Sessions:  []domain.Session{{ID: "a"}, {ID: "b"}},
		Worktrees: []domain.Worktree{{ID: "w"}},
	}}
	_, path := start(t, store)
	st, err := dial(t, path).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != 1234 || st.Sessions != 2 || st.Worktrees != 1 || st.StartedAt.IsZero() {
		t.Fatalf("status %+v", st)
	}
}

func rawCall(t *testing.T, path, line string) rpc.Response {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp rpc.Response
	if err := json.Unmarshal(got, &resp); err != nil {
		t.Fatalf("%s: %v", got, err)
	}
	return resp
}

func TestBadRequestsGetStructuredErrors(t *testing.T) {
	_, path := start(t, &memStore{})
	tests := []struct {
		line string
		id   uint64
		code string
	}{
		{`{"v":2,"id":7,"method":"status"}`, 7, rpc.CodeUnsupportedVersion},
		{`{"id":8,"method":"status"}`, 8, rpc.CodeUnsupportedVersion},
		{fmt.Sprintf(`{"v":1,"id":9,"method":"nope","build":%q}`, version.String()), 9, rpc.CodeUnknownMethod},
		{`not json`, 0, rpc.CodeBadRequest},
	}
	for _, tt := range tests {
		resp := rawCall(t, path, tt.line)
		if resp.V != rpc.Version || resp.ID != tt.id || resp.Error == nil || resp.Error.Code != tt.code || resp.Error.Message == "" {
			t.Errorf("%s: got %+v %+v", tt.line, resp, resp.Error)
		}
	}
}

func TestRequestsFromAnotherBuildAreRefusedNamingTheStaleSide(t *testing.T) {
	_, path := start(t, &memStore{})
	far := time.Now().Add(24 * time.Hour).Unix()
	tests := []struct {
		line string
		want string
	}{
		{fmt.Sprintf(`{"v":1,"id":3,"method":"workspace.list","build":"other","built_at":%d}`, far), "restart the daemon"},
		{`{"v":1,"id":3,"method":"workspace.list","build":"other","built_at":1}`, "restart this client"},
		{`{"v":1,"id":3,"method":"workspace.list"}`, "restart the daemon"},
	}
	for _, tt := range tests {
		resp := rawCall(t, path, tt.line)
		if resp.Error == nil || resp.Error.Code != rpc.CodeVersionMismatch || !strings.Contains(resp.Error.Message, tt.want) {
			t.Errorf("%s: got %+v", tt.line, resp.Error)
		}
	}
}

func TestStatusAndHooksAnswerAnyBuild(t *testing.T) {
	_, path := start(t, &memStore{})
	for _, line := range []string{
		`{"v":1,"id":4,"method":"status","build":"other","built_at":1}`,
		`{"v":1,"id":5,"method":"hook","build":"other","built_at":1,"params":{}}`,
	} {
		resp := rawCall(t, path, line)
		if resp.Error != nil || resp.Build != version.String() {
			t.Errorf("%s: got %+v build %q", line, resp.Error, resp.Build)
		}
	}
}

func TestClientSurfacesServerErrors(t *testing.T) {
	_, path := start(t, &memStore{})
	c := dial(t, path)
	var rerr *rpc.Error
	if err := c.Call(context.Background(), "nope", nil, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnknownMethod {
		t.Fatalf("err %v", err)
	}
}

func TestASlowSubscriberDoesNotStallTheLoop(t *testing.T) {
	d, path := start(t, &memStore{})
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(fmt.Sprintf(`{"v":1,"id":1,"method":"subscribe","build":%q}`, version.String()) + "\n")); err != nil {
		t.Fatal(err)
	}
	blob := strings.Repeat("x", 4096)
	for i := range 5000 {
		d.Post(daemon.SessionChanged{Session: domain.Session{ID: fmt.Sprint(i), Model: blob}})
	}
	waitStatus(t, dial(t, path), func(s rpc.Status) bool { return s.Sessions == 5000 })
}

func TestHookEventUpdatesTheSessionOnItsPane(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	hook := rpc.Hook{Harness: "claude", Event: "Stop", Pane: "%3", At: time.Now(), Payload: json.RawMessage(`{}`)}
	if err := c.Call(ctx, rpc.MethodHook, hook, nil); err != nil {
		t.Fatal(err)
	}
	diff := next(t, sub.Diffs)
	if diff.Session == nil || diff.Session.ID != "a" || diff.Session.State != domain.StateDone {
		t.Fatalf("diff %+v", diff)
	}
}

func TestHookFromAnUnknownPaneOrEventIsIgnored(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	for _, h := range []rpc.Hook{
		{Harness: "claude", Event: "Stop", Pane: "%9"},
		{Harness: "claude", Event: "Bogus", Pane: "%3"},
	} {
		if err := c.Call(ctx, rpc.MethodHook, h, nil); err != nil {
			t.Fatal(err)
		}
	}
	d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: "w"}})
	if diff := next(t, sub.Diffs); diff.Worktree == nil {
		t.Fatalf("hook produced %+v", diff)
	}
}

func TestOnlyOneDaemonHoldsTheLock(t *testing.T) {
	home := shortDir(t)
	first, err := daemon.Acquire(home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = daemon.Acquire(home)
	if !errors.Is(err, daemon.ErrAlreadyRunning) || !strings.Contains(err.Error(), fmt.Sprint(os.Getpid())) {
		t.Fatalf("second acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := daemon.Acquire(home)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = again.Release()
}

func waitServing(t *testing.T, home string, done <-chan error) *rpc.Client {
	t.Helper()
	for {
		if c, err := rpc.Dial(rpc.SocketPath(home)); err == nil {
			return c
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned before serving: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRunServesStateRestoredFromTheStore(t *testing.T) {
	home := shortDir(t)
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.PutSession(domain.Session{ID: "a"})
	store.PutWorktree(domain.Worktree{ID: "w"})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- daemon.Run(ctx, home) }()
		c := waitServing(t, home, done)
		st, err := c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Sessions != 1 || st.Worktrees != 1 {
			t.Fatalf("restored %+v", st)
		}
		_ = c.Close()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func codexRollout(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "adapters", "codex", "testdata", "rollout", "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodexHookReportsModelAtOnceAndEffortAndUsageFromTheRollout(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessCodex, Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	payload, _ := json.Marshal(map[string]string{"model": "gpt-6.1-sol", "transcript_path": codexRollout(t)})
	hook := rpc.Hook{Harness: "codex", Event: "Stop", Pane: "%3", At: time.Now(), Payload: payload}
	if err := c.Call(ctx, rpc.MethodHook, hook, nil); err != nil {
		t.Fatal(err)
	}
	first := next(t, sub.Diffs)
	if first.Session == nil || first.Session.State != domain.StateDone || first.Session.Model != "gpt-6.1-sol" {
		t.Fatalf("first diff %+v", first.Session)
	}
	second := next(t, sub.Diffs)
	if second.Session == nil || second.Session.State != domain.StateDone || second.Session.Effort != "medium" ||
		second.Session.Usage != (domain.Usage{ContextLeftPercent: 56, HasContext: true, LimitUsedPercent: 41}) {
		t.Fatalf("second diff %+v", second.Session)
	}
}

func TestHookTranscriptPathSetsTheSessionTranscriptAndALaterOneReplacesIt(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%3", State: domain.StateIdle}})
	next(t, sub.Diffs)
	for _, step := range []struct{ event, payload, want string }{
		{"SessionStart", `{"session_id":"s1","transcript_path":"/home/dev/.claude/projects/api/s1.jsonl"}`, "/home/dev/.claude/projects/api/s1.jsonl"},
		{"UserPromptSubmit", `{"session_id":"s1"}`, "/home/dev/.claude/projects/api/s1.jsonl"},
		{"SessionStart", `{"session_id":"s2","transcript_path":"/home/dev/.claude/projects/api/s2.jsonl"}`, "/home/dev/.claude/projects/api/s2.jsonl"},
	} {
		hook := rpc.Hook{Harness: "claude", Event: step.event, Pane: "%3", At: time.Now(), Payload: json.RawMessage(step.payload)}
		if err := c.Call(ctx, rpc.MethodHook, hook, nil); err != nil {
			t.Fatal(err)
		}
		diff := next(t, sub.Diffs)
		if diff.Session == nil || diff.Session.Transcript != step.want {
			t.Fatalf("after %s: diff %+v, want transcript %q", step.event, diff.Session, step.want)
		}
	}
}

func TestCodexHookWithAnUnreadableRolloutStillMovesTheState(t *testing.T) {
	d, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessCodex, Pane: "%3", State: domain.StateRunning}})
	next(t, sub.Diffs)
	payload, _ := json.Marshal(map[string]string{"transcript_path": filepath.Join(t.TempDir(), "missing.jsonl")})
	if err := c.Call(ctx, rpc.MethodHook, rpc.Hook{Harness: "codex", Event: "Stop", Pane: "%3", Payload: payload}, nil); err != nil {
		t.Fatal(err)
	}
	if diff := next(t, sub.Diffs); diff.Session == nil || diff.Session.State != domain.StateDone {
		t.Fatalf("diff %+v", diff.Session)
	}
	d.Post(daemon.WorktreeChanged{Worktree: domain.Worktree{ID: "w"}})
	if diff := next(t, sub.Diffs); diff.Worktree == nil {
		t.Fatalf("unexpected diff %+v", diff)
	}
}
