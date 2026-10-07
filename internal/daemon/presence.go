package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	PresenceEvery   = 5 * time.Second
	presenceTimeout = 2 * time.Second
)

type presenceCfg struct {
	activity app.TerminalActivity
	every    time.Duration
}

func WithPresence(a app.TerminalActivity, awayAfter, every time.Duration) Option {
	return func(d *Daemon) {
		d.presence = presenceCfg{activity: a, every: every}
		d.st.presence.AwayAfter = awayAfter
	}
}

func LoadAwayAfter(path string) (time.Duration, error) {
	var cfg struct {
		Push struct {
			AwayAfter *string `toml:"away_after"`
		} `toml:"push"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.DefaultAwayAfter, nil
	}
	if err != nil {
		return domain.DefaultAwayAfter, fmt.Errorf("push: %s: %w", path, err)
	}
	if cfg.Push.AwayAfter == nil {
		return domain.DefaultAwayAfter, nil
	}
	d, err := time.ParseDuration(*cfg.Push.AwayAfter)
	if err != nil || d < 0 {
		return domain.DefaultAwayAfter, fmt.Errorf("push: away_after %q is not a duration such as \"2m\" or \"0\"", *cfg.Push.AwayAfter)
	}
	return d, nil
}

func (d *Daemon) samplePresence(ctx context.Context) {
	var failed string
	for {
		sctx, cancel := context.WithTimeout(ctx, presenceTimeout)
		at, err := d.presence.activity.LastInput(sctx)
		cancel()
		if err != nil {
			if err.Error() != failed {
				log.Printf("presence: %v", err)
			}
			failed = err.Error()
			at = time.Time{}
		} else {
			failed = ""
		}
		if !d.query(func(s *state) { s.notePresence(at, time.Now()) }) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.presence.every):
		}
	}
}

func (s *state) notePresence(lastInput, now time.Time) {
	s.presence.LastInput = lastInput
	for _, b := range s.gate.Release(sorted(s.sessions), s.presence, now) {
		s.enqueuePush(b)
	}
}

func (d *Daemon) deviceViewing(c *conn, req rpc.Request) (*rpc.Response, bool) {
	var p rpc.DeviceViewingParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "device.viewing params: "+err.Error()), true
	}
	found := false
	ok := d.query(func(s *state) {
		if _, found = s.devices[p.Device]; found {
			s.views[c] = domain.ViewReport{Device: p.Device, Visible: p.Visible, At: time.Now()}
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no device "+p.Device), ok
	}
	return result(req.ID, struct{}{}), ok
}

func (s *state) viewing(now time.Time) map[string]bool {
	reports := make([]domain.ViewReport, 0, len(s.views))
	for _, r := range s.views {
		reports = append(reports, r)
	}
	return domain.Viewing(reports, now)
}

func (d *Daemon) clientViewing(c *conn, req rpc.Request) (*rpc.Response, bool) {
	var p rpc.ClientViewingParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "client.viewing params: "+err.Error()), true
	}
	ok := d.query(func(s *state) {
		s.clients[c] = domain.ClientView{Session: p.Session, Front: p.Front}
		s.noteClients(time.Now())
	})
	return result(req.ID, struct{}{}), ok
}

func (s *state) dropClientView(c *conn, now time.Time) {
	if _, ok := s.clients[c]; !ok {
		return
	}
	delete(s.clients, c)
	s.noteClients(now)
}

func (s *state) noteClients(now time.Time) {
	front := domain.AppFront(s.clientViews())
	if s.presence.AppFront && !front {
		s.presence.AppLeft = now
	}
	s.presence.AppFront = front
}

func (s *state) clientViews() []domain.ClientView {
	views := make([]domain.ClientView, 0, len(s.clients))
	for _, v := range s.clients {
		views = append(views, v)
	}
	return views
}
