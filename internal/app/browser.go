package app

import (
	"context"
	"encoding/json"

	"github.com/giovaniif/crew/internal/domain"
)

type HeadlessBrowsers interface {
	Open(ctx context.Context, session string) (HeadlessTab, error)
}

type HeadlessTab interface {
	Run(ctx context.Context, command string, args json.RawMessage) (json.RawMessage, error)
	SetCookies(ctx context.Context, cookies []domain.BrowserCookie) error
	Close() error
}
