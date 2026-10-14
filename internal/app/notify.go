package app

import (
	"context"

	"github.com/giovaniif/crew/internal/domain"
)

type Notifier interface {
	Notify(ctx context.Context, b domain.Banner) error
	Remove(ctx context.Context, group string) error
}

type Foreground interface {
	TerminalFrontmost(ctx context.Context) bool
}
