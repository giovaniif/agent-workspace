package rpc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/version"
)

func serveStream(t *testing.T, path string, answer func(req rpc.Request, send func(rpc.Response))) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		sc := bufio.NewScanner(conn)
		enc := json.NewEncoder(conn)
		send := func(resp rpc.Response) {
			resp.V, resp.Build = rpc.Version, version.String()
			_ = enc.Encode(resp)
		}
		for sc.Scan() {
			var req rpc.Request
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				return
			}
			answer(req, send)
		}
	}()
}

func dialRPC(t *testing.T, path string) *rpc.Client {
	t.Helper()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestTranscriptMessagesEncodeWithSnakeCaseFields(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	msg := rpc.MessageOf(domain.Message{ID: "c1", Cursor: 42, Turn: "p1", Role: domain.RoleTool, Text: "PASS", At: at,
		Tool: &domain.MessageTool{Name: "Bash", Summary: "Bash go test", Status: domain.ToolDone}})
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"c1","cursor":42,"turn":"p1","role":"tool","text":"PASS","tool":{"name":"Bash","summary":"Bash go test","status":"done"},"at":"2026-10-03T12:00:00Z"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	plain, _ := json.Marshal(rpc.MessageOf(domain.Message{ID: "u1", Cursor: 7, Role: domain.RoleUser, Text: "hi", At: at}))
	if string(plain) != `{"id":"u1","cursor":7,"role":"user","text":"hi","at":"2026-10-03T12:00:00Z"}` {
		t.Fatalf("plain %s", plain)
	}
	if got := rpc.MessagesOf(nil); got == nil || len(got) != 0 {
		t.Fatalf("MessagesOf(nil) = %#v", got)
	}
}

func TestTranscriptPageClientSendsItsParams(t *testing.T) {
	path := filepath.Join(shortDir(t), "agentws.sock")
	got := make(chan rpc.Request, 1)
	serveStream(t, path, func(req rpc.Request, send func(rpc.Response)) {
		got <- req
		result, _ := json.Marshal(rpc.TranscriptPage{Messages: []rpc.Message{{ID: "u1", Cursor: 9, Role: "user"}}, Before: 3})
		send(rpc.Response{ID: req.ID, Result: result})
	})
	page, err := dialRPC(t, path).TranscriptPage(context.Background(), "s1", 120, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "u1" || page.Before != 3 {
		t.Fatalf("page %+v", page)
	}
	req := <-got
	var p rpc.TranscriptPageParams
	_ = json.Unmarshal(req.Params, &p)
	if req.Method != rpc.MethodTranscriptPage || p != (rpc.TranscriptPageParams{Session: "s1", Before: 120, Limit: 25}) {
		t.Fatalf("request %s %+v", req.Method, p)
	}
}

func TestTranscriptWatchClientStreamsEventsAndUnwatchesOnCancel(t *testing.T) {
	path := filepath.Join(shortDir(t), "agentws.sock")
	unwatched := make(chan rpc.TranscriptUnwatchParams, 1)
	var watchID uint64
	serveStream(t, path, func(req rpc.Request, send func(rpc.Response)) {
		switch req.Method {
		case rpc.MethodTranscriptWatch:
			watchID = req.ID
			var p rpc.TranscriptWatchParams
			_ = json.Unmarshal(req.Params, &p)
			if p != (rpc.TranscriptWatchParams{Session: "s1", After: 64}) {
				t.Errorf("watch params %+v", p)
			}
			result, _ := json.Marshal(rpc.TranscriptWatched{Messages: []rpc.Message{{ID: "a1", Cursor: 80}}})
			send(rpc.Response{ID: req.ID, Result: result})
			send(rpc.Response{ID: req.ID, Transcript: &rpc.TranscriptEvent{Messages: []rpc.Message{{ID: "a2", Cursor: 99}}}})
			send(rpc.Response{ID: req.ID, Transcript: &rpc.TranscriptEvent{Reset: true}})
		case rpc.MethodTranscriptUnwatch:
			var p rpc.TranscriptUnwatchParams
			_ = json.Unmarshal(req.Params, &p)
			unwatched <- p
			send(rpc.Response{ID: req.ID, Result: json.RawMessage(`{}`)})
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	w, err := dialRPC(t, path).WatchTranscript(ctx, "s1", 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Messages) != 1 || w.Messages[0].ID != "a1" {
		t.Fatalf("initial %+v", w.Messages)
	}
	var events []rpc.TranscriptEvent
	for len(events) < 2 {
		select {
		case ev := <-w.Events:
			events = append(events, ev)
		case <-time.After(2 * time.Second):
			t.Fatalf("events so far %+v", events)
		}
	}
	want := []rpc.TranscriptEvent{{Messages: []rpc.Message{{ID: "a2", Cursor: 99}}}, {Reset: true}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events %+v", events)
	}
	cancel()
	select {
	case p := <-unwatched:
		if p.Watch != watchID {
			t.Fatalf("unwatched %d, watch was %d", p.Watch, watchID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no unwatch")
	}
	select {
	case _, ok := <-w.Events:
		if ok {
			t.Fatal("event after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events not closed")
	}
}

func TestTranscriptWatchClientClosesEventsAfterAClosedEvent(t *testing.T) {
	path := filepath.Join(shortDir(t), "agentws.sock")
	serveStream(t, path, func(req rpc.Request, send func(rpc.Response)) {
		send(rpc.Response{ID: req.ID, Result: json.RawMessage(`{"messages":[]}`)})
		send(rpc.Response{ID: req.ID, Transcript: &rpc.TranscriptEvent{Closed: true}})
	})
	w, err := dialRPC(t, path).WatchTranscript(context.Background(), "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := <-w.Events
	if !ok || !ev.Closed {
		t.Fatalf("event %+v %v", ev, ok)
	}
	select {
	case _, ok := <-w.Events:
		if ok {
			t.Fatal("event after closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events not closed")
	}
}

func TestTranscriptWatchClientReturnsTheDaemonsError(t *testing.T) {
	path := filepath.Join(shortDir(t), "agentws.sock")
	serveStream(t, path, func(req rpc.Request, send func(rpc.Response)) {
		send(rpc.Response{ID: req.ID, Error: &rpc.Error{Code: rpc.CodeNotFound, Message: "no session s9"}})
	})
	_, err := dialRPC(t, path).WatchTranscript(context.Background(), "s9", 0)
	if rerr, ok := err.(*rpc.Error); !ok || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("err %v", err)
	}
}
