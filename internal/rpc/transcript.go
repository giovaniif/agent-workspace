package rpc

import (
	"context"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const (
	MethodTranscriptPage    = "transcript.page"
	MethodTranscriptWatch   = "transcript.watch"
	MethodTranscriptUnwatch = "transcript.unwatch"
)

type MessageTool struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status"`
}

type Message struct {
	ID     string       `json:"id"`
	Cursor int64        `json:"cursor"`
	Turn   string       `json:"turn,omitempty"`
	Role   string       `json:"role"`
	Text   string       `json:"text,omitempty"`
	Tool   *MessageTool `json:"tool,omitempty"`
	At     time.Time    `json:"at"`
}

func MessageOf(m domain.Message) Message {
	out := Message{ID: m.ID, Cursor: m.Cursor, Turn: m.Turn, Role: string(m.Role), Text: m.Text, At: m.At}
	if m.Tool != nil {
		out.Tool = &MessageTool{Name: m.Tool.Name, Summary: m.Tool.Summary, Status: string(m.Tool.Status)}
	}
	return out
}

func MessagesOf(msgs []domain.Message) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, MessageOf(m))
	}
	return out
}

type TranscriptPageParams struct {
	Session string `json:"session"`
	Before  int64  `json:"before,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type TranscriptPage struct {
	Messages []Message `json:"messages"`
	Before   int64     `json:"before"`
}

type TranscriptWatchParams struct {
	Session string `json:"session"`
	After   int64  `json:"after"`
}

type TranscriptWatched struct {
	Messages []Message `json:"messages"`
}

type TranscriptUnwatchParams struct {
	Watch uint64 `json:"watch"`
}

type TranscriptEvent struct {
	Messages []Message `json:"messages,omitempty"`
	Reset    bool      `json:"reset,omitempty"`
	Closed   bool      `json:"closed,omitempty"`
}

func (c *Client) TranscriptPage(ctx context.Context, session string, before int64, limit int) (TranscriptPage, error) {
	var out TranscriptPage
	err := c.Call(ctx, MethodTranscriptPage, TranscriptPageParams{Session: session, Before: before, Limit: limit}, &out)
	return out, err
}

type TranscriptWatch struct {
	Messages []Message
	Events   <-chan TranscriptEvent
}

func (c *Client) WatchTranscript(ctx context.Context, session string, after int64) (TranscriptWatch, error) {
	id, ch, err := c.send(MethodTranscriptWatch, TranscriptWatchParams{Session: session, After: after})
	if err != nil {
		return TranscriptWatch{}, err
	}
	resp, err := c.await(ctx, ch)
	if err == nil {
		err = checkBuild(MethodTranscriptWatch, resp)
	}
	var watched TranscriptWatched
	if err == nil {
		err = decode(resp, &watched)
	}
	if err != nil {
		c.forget(id)
		return TranscriptWatch{}, err
	}
	events := make(chan TranscriptEvent, 64)
	go c.forwardTranscript(ctx, id, ch, events)
	return TranscriptWatch{Messages: watched.Messages, Events: events}, nil
}

func (c *Client) forwardTranscript(ctx context.Context, id uint64, ch chan Response, events chan<- TranscriptEvent) {
	defer close(events)
	unwatch := func() {
		c.forget(id)
		if uid, _, err := c.send(MethodTranscriptUnwatch, TranscriptUnwatchParams{Watch: id}); err == nil {
			c.forget(uid)
		}
	}
	for {
		var resp Response
		var ok bool
		select {
		case <-ctx.Done():
			unwatch()
			return
		case resp, ok = <-ch:
		}
		if !ok {
			return
		}
		if resp.Transcript == nil {
			continue
		}
		select {
		case events <- *resp.Transcript:
		case <-ctx.Done():
			unwatch()
			return
		}
		if resp.Transcript.Closed {
			c.forget(id)
			return
		}
	}
}
