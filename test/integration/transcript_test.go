//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	wsfs "github.com/giovaniif/agent-workspace/internal/adapters/fs"
	"github.com/giovaniif/agent-workspace/internal/adapters/sqlite"
	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func claudeUser(id, text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"promptId":"p-%s","timestamp":"2026-10-03T12:00:00Z","message":{"role":"user","content":%q}}`+"\n", id, id, text)
}

func claudeAssistant(id, text string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-10-03T12:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", id, text)
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func serveTranscripts(t *testing.T, home string) (*daemon.Daemon, *rpc.Client) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := daemon.New(store, os.Getpid(), daemon.WithTranscripts(daemon.Transcripts(wsfs.Transcripts{}), wsfs.Transcripts{}))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "agentws.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Serve(ctx, ln)
		_ = store.Close()
		close(done)
	}()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		<-done
	})
	return d, c
}

func messageIDs(msgs []rpc.Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func TestTranscriptPageAndWatchOnARealClaudeTranscript(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "agentws-tx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	transcript := filepath.Join(home, "projects", "api", "s1.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, transcript, claudeUser("u1", "fix the test")+claudeAssistant("a1", "on it")+claudeAssistant("a2", "done"))
	d, c := serveTranscripts(t, home)
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s", Harness: domain.HarnessClaude, Pane: "%1", State: domain.StateIdle, Transcript: transcript}})
	ctx := context.Background()

	var page rpc.TranscriptPage
	if !eventually(t, 2*time.Second, func() bool {
		page, err = c.TranscriptPage(ctx, "s", 0, 2)
		return err == nil
	}) {
		t.Fatalf("page: %v", err)
	}
	if !reflect.DeepEqual(messageIDs(page.Messages), []string{"a1", "a2"}) || page.Before == 0 {
		t.Fatalf("newest page %v before %d", messageIDs(page.Messages), page.Before)
	}
	older, err := c.TranscriptPage(ctx, "s", page.Before, 2)
	if err != nil || !reflect.DeepEqual(messageIDs(older.Messages), []string{"u1"}) || older.Before != 0 {
		t.Fatalf("older page %v before %d: %v", messageIDs(older.Messages), older.Before, err)
	}

	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w, err := c.WatchTranscript(wctx, "s", page.Messages[1].Cursor)
	if err != nil || len(w.Messages) != 0 {
		t.Fatalf("watch: %v %v", w.Messages, err)
	}
	line := claudeAssistant("a3", "one more thing")
	appendFile(t, transcript, line[:30])
	select {
	case ev := <-w.Events:
		t.Fatalf("event for a partial line: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
	appendFile(t, transcript, line[30:])
	select {
	case ev := <-w.Events:
		if !reflect.DeepEqual(messageIDs(ev.Messages), []string{"a3"}) {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event for the appended line")
	}
}
