package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type RelayTarget struct {
	Notifier   app.Notifier
	Foreground app.Foreground
	Terminal   string
}

func Relay(ctx context.Context, r io.Reader, t RelayTarget) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), rpc.MaxMessage)
	for sc.Scan() {
		var n rpc.Notice
		if err := json.Unmarshal(sc.Bytes(), &n); err != nil {
			log.Printf("notify relay: %v", err)
			continue
		}
		if err := deliverNotice(ctx, n, t); err != nil {
			log.Printf("notify relay: %v", err)
		}
	}
	return sc.Err()
}

func deliverNotice(ctx context.Context, n rpc.Notice, t RelayTarget) error {
	if n.Remove != "" {
		return t.Notifier.Remove(ctx, n.Remove)
	}
	if n.Banner == nil || (n.Focused && t.Foreground != nil && t.Foreground.TerminalFrontmost(ctx)) {
		return nil
	}
	b := *n.Banner
	b.Terminal = t.Terminal
	return t.Notifier.Notify(ctx, b)
}

type Silent struct{}

func (Silent) Notify(context.Context, domain.Banner) error { return nil }
func (Silent) Remove(context.Context, string) error        { return nil }
