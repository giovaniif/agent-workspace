package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

// why: the harness must take the whole paste before it sees Enter.
const PasteSettle = 150 * time.Millisecond

const (
	// why: covers the quick model popup, the "All models" list, the reasoning
	// level popup and one spare.
	codexPickerSteps = 4
	codexPickerPolls = 20
	codexPickerLines = 40
	escapeTimeout    = time.Second
)

var errNoCodexPicker = errors.New("the Codex /model picker did not open")

func SendSwitches(ctx context.Context, host TerminalHost, s domain.Session, sws []domain.Switch, settle time.Duration) error {
	pane := PaneID(s.Pane)
	for _, sw := range sws {
		text, ok := s.SwitchCommand(sw)
		if !ok {
			return fmt.Errorf("no %s switch text for %s", sw.Kind, s.Harness)
		}
		if err := host.SendText(ctx, pane, text, true); err != nil {
			return err
		}
		if err := wait(ctx, settle); err != nil {
			return err
		}
		if err := host.SendKeys(ctx, pane, "Enter"); err != nil {
			return err
		}
		if s.Harness != domain.HarnessCodex {
			continue
		}
		if err := walkCodexPicker(ctx, host, pane, s, sw, settle); err != nil {
			return err
		}
		if sw.Kind == domain.SwitchModel {
			s.Model = sw.Value
		} else {
			s.Effort = sw.Value
		}
	}
	return nil
}

// bug: tmux returns before Codex redraws and a stale screen would steer the
// next keys into the wrong popup, so it waits for the picker to change. Anything
// it cannot place closes the picker with Escape, so no half-made choice stays open.
func walkCodexPicker(ctx context.Context, host TerminalHost, pane PaneID, s domain.Session, sw domain.Switch, settle time.Duration) error {
	var last *domain.Picker
	for range codexPickerSteps {
		picker, open, err := nextPicker(ctx, host, pane, last, settle)
		switch {
		case err != nil:
			return errors.Join(err, escape(host, pane))
		case !open && last == nil:
			return errNoCodexPicker
		case !open:
			return nil
		}
		keys, err := domain.CodexPickerKeys(picker, sw, s.Model, s.Effort)
		if err != nil {
			return errors.Join(err, escape(host, pane))
		}
		if err := host.SendKeys(ctx, pane, keys...); err != nil {
			return errors.Join(err, escape(host, pane))
		}
		last = &picker
	}
	return errors.Join(errors.New("the Codex picker is still open"), escape(host, pane))
}

func nextPicker(ctx context.Context, host TerminalHost, pane PaneID, last *domain.Picker, settle time.Duration) (domain.Picker, bool, error) {
	for range codexPickerPolls {
		if err := wait(ctx, settle); err != nil {
			return domain.Picker{}, false, err
		}
		screen, err := host.Capture(ctx, pane, codexPickerLines)
		if err != nil {
			return domain.Picker{}, false, err
		}
		picker, open := domain.ParsePicker(screen)
		switch {
		case open && (last == nil || !reflect.DeepEqual(picker, *last)):
			return picker, true, nil
		case !open && last != nil:
			return domain.Picker{}, false, nil
		}
	}
	if last == nil {
		return domain.Picker{}, false, nil
	}
	return domain.Picker{}, false, errors.New("the Codex picker did not redraw")
}

// why: its own context, because the caller's may be the one that ran out.
func escape(host TerminalHost, pane PaneID) error {
	ctx, cancel := context.WithTimeout(context.Background(), escapeTimeout)
	defer cancel()
	return host.SendKeys(ctx, pane, "Escape")
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
