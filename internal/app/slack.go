package app

import (
	"context"

	"github.com/giovaniif/crew/internal/domain"
)

type SlackEventKind string

const (
	SlackMention       SlackEventKind = "mention"
	SlackThreadMessage SlackEventKind = "thread_message"
	SlackDirectMessage SlackEventKind = "direct_message"
	SlackAction        SlackEventKind = "action"
)

type SlackEvent struct {
	Kind     SlackEventKind
	Team     string
	Channel  string
	User     string
	Text     string
	TS       string
	ThreadTS string
	ActionID string
	Value    string
}

type SlackMessage struct {
	User string
	Text string
	TS   string
	Bot  bool
}

type SlackStatus struct {
	State domain.MateConnection
	Error string
	User  string
}

type SlackMate interface {
	Run(ctx context.Context, handle func(SlackEvent), status func(SlackStatus)) error
	PostMessage(ctx context.Context, channel, threadTS, text string) (string, error)
	PostApproval(ctx context.Context, channel, threadTS, text, id string) (string, error)
	UpdateMessage(ctx context.Context, channel, ts, text string) error
	Replies(ctx context.Context, channel, threadTS string) ([]SlackMessage, error)
	UserName(ctx context.Context, user string) (string, error)
}

type SlackConnector func(bot, app string) SlackMate

type MateTokenReader interface {
	MateTokens(name string) (bot, app string, err error)
}
