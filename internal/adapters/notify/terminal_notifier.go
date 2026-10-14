package notify

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

var _ app.Notifier = TerminalNotifier{}

type TerminalNotifier struct {
	Run      Runner
	Bin      string
	Self     string
	Home     string
	FocusCmd string
}

func (n TerminalNotifier) Notify(ctx context.Context, b domain.Banner) error {
	args := []string{"-title", b.Title, "-message", escapeMessage(b.Body)}
	if b.Group != "" {
		args = append(args, "-group", b.Group)
	}
	if b.Sound != "" {
		args = append(args, "-sound", b.Sound)
	}
	if b.Terminal != "" {
		args = append(args, "-activate", b.Terminal)
	}
	if b.Group != "" && n.FocusCmd != "" {
		args = append(args, "-execute", n.FocusCmd+" "+shellQuote(b.Group))
	} else if b.Group != "" && n.Self != "" {
		args = append(args, "-execute", domain.EnvName("HOME")+"="+shellQuote(n.Home)+" "+shellQuote(n.Self)+" focus "+shellQuote(b.Group))
	}
	_, err := n.Run(ctx, n.Bin, args...)
	return err
}

func (n TerminalNotifier) Remove(ctx context.Context, group string) error {
	_, err := n.Run(ctx, n.Bin, "-remove", group)
	return err
}

func escapeMessage(s string) string {
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "-") {
		return `\` + s
	}
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type Click struct {
	Self     string
	Home     string
	FocusCmd string
}

func Select(lookPath func(string) (string, error), click Click) app.Notifier {
	_, osaErr := lookPath("osascript")
	if bin, err := lookPath("terminal-notifier"); err == nil {
		tn := TerminalNotifier{Run: execRunner, Bin: bin, Self: click.Self, Home: click.Home, FocusCmd: click.FocusCmd}
		if osaErr != nil {
			return tn
		}
		return Fallback{Primary: tn, Secondary: New()}
	}
	if osaErr == nil {
		return New()
	}
	return Silent{}
}

func Detect(click Click) app.Notifier { return Select(exec.LookPath, click) }

type Fallback struct {
	Primary   app.Notifier
	Secondary app.Notifier
}

func (f Fallback) Notify(ctx context.Context, b domain.Banner) error {
	err := f.Primary.Notify(ctx, b)
	if err == nil {
		return nil
	}
	log.Printf("notify: %v; posting with the fallback", err)
	if err2 := f.Secondary.Notify(ctx, b); err2 != nil {
		return errors.Join(err, err2)
	}
	return nil
}

func (f Fallback) Remove(ctx context.Context, group string) error {
	return f.Primary.Remove(ctx, group)
}
