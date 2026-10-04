package daemon_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const transcriptA = "/home/dev/.claude/projects/api/s1.jsonl"

func userLine(id, text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"promptId":"p-%s","timestamp":"2026-10-03T12:00:00Z","message":{"role":"user","content":%q}}`+"\n", id, id, text)
}

func assistantLine(id, text string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-10-03T12:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", id, text)
}

func toolUseLine(id, call string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-10-03T12:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Bash","input":{"command":"go test"}}]}}`+"\n", id, call)
}

func toolResultLine(id, call, out string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-10-03T12:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`+"\n", id, call, out)
}

func startTranscripts(t *testing.T, files *memTranscripts) (*daemon.Daemon, string) {
	t.Helper()
	tr := app.Transcripts{Files: files, Parsers: map[domain.Harness]func() app.TranscriptParser{
		domain.HarnessClaude: func() app.TranscriptParser { return &claude.TranscriptParser{} },
	}}
	return start(t, &memStore{}, daemon.WithTranscripts(tr, files))
}

func addSession(t *testing.T, d *daemon.Daemon, c *rpc.Client, id, transcript string) {
	t.Helper()
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: id, Harness: domain.HarnessClaude, Pane: "%" + id, State: domain.StateIdle, Transcript: transcript}})
	waitFor(t, func() bool {
		st, err := c.Status(context.Background())
		return err == nil && st.Sessions > 0
	})
}

func msgIDs(msgs []rpc.Message) []string {
	var out []string
	for _, m := range msgs {
		s := m.ID
		if m.Tool != nil {
			s += "=" + m.Tool.Status
		}
		out = append(out, s)
	}
	return out
}

func nextEvent(t *testing.T, events <-chan rpc.TranscriptEvent) rpc.TranscriptEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("transcript events closed")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no transcript event")
	}
	return rpc.TranscriptEvent{}
}

func noEvent(t *testing.T, events <-chan rpc.TranscriptEvent) {
	t.Helper()
	select {
	case ev := <-events:
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTranscriptPageOfAnUnknownSessionIsNotFoundAndOfOneWithoutATranscriptIsEmpty(t *testing.T) {
	files := newMemTranscripts()
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	ctx := context.Background()
	if _, err := c.TranscriptPage(ctx, "nope", 0, 10); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("unknown session: %v", err)
	}
	addSession(t, d, c, "a", "")
	page, err := c.TranscriptPage(ctx, "a", 0, 10)
	if err != nil || page.Messages == nil || len(page.Messages) != 0 || page.Before != 0 {
		t.Fatalf("no transcript: %+v %v", page, err)
	}
	if _, err := c.WatchTranscript(ctx, "nope", 0); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("watch of an unknown session: %v", err)
	}
}

func TestTranscriptPageWalksBackFromTheNewestPageToTheFirstMessage(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "fix the test")+assistantLine("a1", "reading")+toolUseLine("a2", "c1")+
		assistantLine("a3", "waiting")+toolResultLine("r1", "c1", "PASS")+userLine("u2", "thanks")+assistantLine("a4", "done"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", transcriptA)
	var all []string
	var before int64
	for i := 0; ; i++ {
		page, err := c.TranscriptPage(context.Background(), "a", before, 2)
		if err != nil {
			t.Fatal(err)
		}
		all = append(msgIDs(page.Messages), all...)
		if page.Before == 0 {
			break
		}
		if i > 10 {
			t.Fatal("paging never reached the first message")
		}
		before = page.Before
	}
	if want := []string{"u1", "a1", "c1=done", "a3", "u2", "a4"}; !reflect.DeepEqual(all, want) {
		t.Fatalf("pages %v, want %v", all, want)
	}
	if _, err := c.TranscriptPage(context.Background(), "a", 5, 2); errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("mid-line cursor: %v", err)
	}
}

func TestTranscriptWatchGetsAppendedMessagesAndAPartialLineOnceComplete(t *testing.T) {
	files := newMemTranscripts()
	first := userLine("u1", "hello")
	files.write(transcriptA, first+assistantLine("a1", "hi"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", transcriptA)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := c.WatchTranscript(ctx, "a", int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msgIDs(w.Messages), []string{"a1"}) {
		t.Fatalf("initial %v", msgIDs(w.Messages))
	}
	line := assistantLine("a2", "more")
	files.write(transcriptA, line[:20])
	noEvent(t, w.Events)
	files.write(transcriptA, line[20:])
	if ev := nextEvent(t, w.Events); !reflect.DeepEqual(msgIDs(ev.Messages), []string{"a2"}) || ev.Reset {
		t.Fatalf("event %+v", ev)
	}
	files.write(transcriptA, toolUseLine("a3", "c1"))
	if ev := nextEvent(t, w.Events); !reflect.DeepEqual(msgIDs(ev.Messages), []string{"c1=running"}) {
		t.Fatalf("call %+v", ev)
	}
	files.write(transcriptA, toolResultLine("r1", "c1", "ok"))
	if ev := nextEvent(t, w.Events); !reflect.DeepEqual(msgIDs(ev.Messages), []string{"c1=done"}) || ev.Messages[0].Tool.Name != "Bash" {
		t.Fatalf("result %+v", ev)
	}
}

func TestTranscriptWatchersOfOneSessionShareOneTailerThatStopsWhenBothLeave(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "hello"))
	d, path := startTranscripts(t, files)
	one, two := dial(t, path), dial(t, path)
	addSession(t, d, one, "a", transcriptA)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()
	w1, err := one.WatchTranscript(ctx1, "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := two.WatchTranscript(ctx2, "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msgIDs(w1.Messages), []string{"u1"}) || !reflect.DeepEqual(msgIDs(w2.Messages), []string{"u1"}) {
		t.Fatalf("initial %v %v", msgIDs(w1.Messages), msgIDs(w2.Messages))
	}
	files.write(transcriptA, assistantLine("a1", "hi"))
	for _, w := range []rpc.TranscriptWatch{w1, w2} {
		if ev := nextEvent(t, w.Events); !reflect.DeepEqual(msgIDs(ev.Messages), []string{"a1"}) {
			t.Fatalf("event %+v", ev)
		}
	}
	if _, watches, active := files.counts(); watches != 1 || active != 1 {
		t.Fatalf("watches %d active %d, want one shared", watches, active)
	}
	cancel1()
	time.Sleep(50 * time.Millisecond)
	if _, _, active := files.counts(); active != 1 {
		t.Fatalf("active %d after one left", active)
	}
	files.write(transcriptA, assistantLine("a2", "still here"))
	if ev := nextEvent(t, w2.Events); !reflect.DeepEqual(msgIDs(ev.Messages), []string{"a2"}) {
		t.Fatalf("event after one left %+v", ev)
	}
	cancel2()
	waitFor(t, func() bool { _, _, active := files.counts(); return active == 0 })
}

func TestTranscriptIsNeverOpenedWithoutWatchers(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "hello"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", transcriptA)
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%a", State: domain.StateRunning, Transcript: transcriptA + ".2"}})
	files.write(transcriptA, assistantLine("a1", "hi"))
	time.Sleep(50 * time.Millisecond)
	if touched, watches, _ := files.counts(); touched != 0 || watches != 0 {
		t.Fatalf("touched %d watches %d with no watcher", touched, watches)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := c.WatchTranscript(ctx, "a", 0); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitFor(t, func() bool { _, _, active := files.counts(); return active == 0 })
	touched, _, _ := files.counts()
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%a", State: domain.StateRunning, Transcript: transcriptA}})
	files.write(transcriptA+".2", assistantLine("a2", "hi"))
	time.Sleep(50 * time.Millisecond)
	if after, _, _ := files.counts(); after != touched {
		t.Fatalf("touched %d times after the last watcher left (was %d)", after, touched)
	}
}

func TestTranscriptWatchersSwitchToTheNewFileWhenTheTranscriptChanges(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "hello"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := c.WatchTranscript(ctx, "a", 0)
	if err != nil || len(w.Messages) != 0 {
		t.Fatalf("watch without a transcript: %+v %v", w.Messages, err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%a", State: domain.StateRunning, Transcript: transcriptA}})
	if ev := nextEvent(t, w.Events); !ev.Reset || !reflect.DeepEqual(msgIDs(ev.Messages), []string{"u1"}) {
		t.Fatalf("first transcript %+v", ev)
	}
	next := strings.Replace(transcriptA, "s1", "s2", 1)
	files.write(next, userLine("v1", "resumed"))
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Pane: "%a", State: domain.StateRunning, Transcript: next}})
	if ev := nextEvent(t, w.Events); !ev.Reset || !reflect.DeepEqual(msgIDs(ev.Messages), []string{"v1"}) {
		t.Fatalf("switched %+v", ev)
	}
	files.write(transcriptA, assistantLine("a1", "old file"))
	noEvent(t, w.Events)
	files.write(next, assistantLine("b1", "new file"))
	if ev := nextEvent(t, w.Events); ev.Reset || !reflect.DeepEqual(msgIDs(ev.Messages), []string{"b1"}) {
		t.Fatalf("new file %+v", ev)
	}
}

func TestTranscriptWatchEndsWhenTheSessionIsRemoved(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "hello"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", transcriptA)
	w, err := c.WatchTranscript(context.Background(), "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: domain.HarnessClaude, Ended: true}})
	if ev := nextEvent(t, w.Events); !ev.Closed {
		t.Fatalf("event %+v", ev)
	}
	waitFor(t, func() bool { _, _, active := files.counts(); return active == 0 })
}

func TestTranscriptWatchFromACursorPastTheEndIsABadRequest(t *testing.T) {
	files := newMemTranscripts()
	files.write(transcriptA, userLine("u1", "hello"))
	d, path := startTranscripts(t, files)
	c := dial(t, path)
	addSession(t, d, c, "a", transcriptA)
	if _, err := c.WatchTranscript(context.Background(), "a", 1<<20); errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("watch past the end: %v", err)
	}
	waitFor(t, func() bool { _, _, active := files.counts(); return active == 0 })
}
