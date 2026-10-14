package daemon

import (
	"reflect"
	"time"

	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/domain"
)

const (
	usageTailBytes = 512 << 10
	usageThrottle  = 2 * time.Second
)

type usageJob struct {
	running   bool
	again     bool
	lastStart time.Time
}

func (d *Daemon) requestUsage(s *state, sessionID, path string, force bool) {
	job := s.usage[sessionID]
	if job == nil {
		job = &usageJob{}
		s.usage[sessionID] = job
	}
	if job.running {
		job.again = true
		return
	}
	if !force && time.Since(job.lastStart) < usageThrottle {
		return
	}
	job.running = true
	job.lastStart = time.Now()
	go d.readUsage(sessionID, path)
}

func (d *Daemon) readUsage(sessionID, path string) {
	for {
		snap, err := codex.ReadSnapshotFile(path, usageTailBytes)
		again := false
		ran := d.query(func(s *state) {
			job := s.usage[sessionID]
			if current, ok := s.sessions[sessionID]; ok && err == nil {
				if next := withSnapshot(current, snap); next.Model != current.Model ||
					next.Effort != current.Effort || next.Usage != current.Usage ||
					!next.LimitsAt.Equal(current.LimitsAt) || next.SwitchWarning != current.SwitchWarning ||
					!reflect.DeepEqual(next.Switches, current.Switches) {
					s.emit(SessionChanged{Session: next})
				}
			}
			again, job.again = job.again, false
			job.running = again
		})
		if !ran || !again {
			return
		}
	}
}

func withSnapshot(s domain.Session, snap codex.Snapshot) domain.Session {
	s = s.Report(domain.StatusReport{Model: snap.Model, Effort: snap.Effort, At: snap.TurnAt})
	usage := snap.Usage()
	if snap.ContextLeftPercent != codex.UnknownPercent {
		s.Usage.ContextLeftPercent, s.Usage.HasContext = usage.ContextLeftPercent, true
	}
	if len(snap.Limits) > 0 {
		s.Usage.LimitUsedPercent = usage.LimitUsedPercent
		limits := snap.RateLimits()
		at := snap.LimitsAt
		if at.IsZero() {
			at = time.Now()
			if reflect.DeepEqual(limits, s.Limits) && !s.LimitsAt.IsZero() {
				at = s.LimitsAt
			}
		}
		s.Limits, s.LimitsAt = limits, at
	}
	return s
}
