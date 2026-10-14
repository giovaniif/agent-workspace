package daemon

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const scheduleRecheck = time.Minute

func WithAutomationSchedule(every time.Duration) Option {
	return func(d *Daemon) { d.auto.scheduleEvery = every }
}

func automationRepoOf(a domain.Automation) string {
	if filepath.Base(a.Source) == repoConfigFile {
		return filepath.Dir(a.Source)
	}
	return ""
}

func (d *Daemon) runSchedules(ctx context.Context) {
	firstSeen := map[string]time.Time{}
	for {
		wait := d.scheduleTick(firstSeen)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (d *Daemon) scheduleTick(firstSeen map[string]time.Time) time.Duration {
	wait := d.auto.scheduleEvery
	now := d.ws.now()
	var trust map[string]string
	var runs []domain.AutomationRun
	d.query(func(s *state) {
		trust = map[string]string{}
		for k, v := range s.automationTrust {
			trust[k] = v
		}
		for _, r := range s.automationRuns {
			if r.Trigger == domain.AutomationTriggerSchedule {
				runs = append(runs, r)
			}
		}
	})
	for _, a := range d.automations("").Automations {
		if a.Schedule == "" || !domain.AutomationCanRunAlone(a, trust) {
			continue
		}
		sched, err := domain.ParseSchedule(a.Schedule)
		if err != nil {
			continue
		}
		loc := time.Local
		if a.Timezone != "" {
			if loc, err = time.LoadLocation(a.Timezone); err != nil {
				log.Printf("automation %s: timezone %q: %v", a.Name, a.Timezone, err)
				continue
			}
		}
		repo := automationRepoOf(a)
		key := domain.AutomationTrustKey(a)
		since, found := lastScheduled(runs, a.Name, repo)
		if !found {
			if _, seen := firstSeen[key]; !seen {
				firstSeen[key] = now
			}
			since = firstSeen[key]
		}
		action, due := domain.ScheduleDecision(sched, loc, since, now, domain.DefaultMissedGrace)
		switch action {
		case domain.ScheduleWait:
			if !due.IsZero() {
				wait = min(wait, max(due.Sub(now), time.Millisecond))
			}
		case domain.ScheduleMissed:
			d.recordScheduled(domain.AutomationRun{
				ID: newID(), Automation: a.Name, Trigger: domain.AutomationTriggerSchedule, Repo: repo,
				Start: due, End: due, Outcome: domain.AutomationMissed, Reason: "the daemon was not running at " + due.Format(time.RFC3339),
			})
		case domain.ScheduleRun:
			d.runScheduled(a, repo, due)
		}
	}
	return min(wait, scheduleRecheck)
}

func lastScheduled(runs []domain.AutomationRun, name, repo string) (time.Time, bool) {
	var last time.Time
	found := false
	for _, r := range runs {
		if r.Automation == name && r.Repo == repo && (!found || r.Start.After(last)) {
			last, found = r.Start, true
		}
	}
	return last, found
}

func (d *Daemon) recordScheduled(run domain.AutomationRun) {
	d.query(func(s *state) { s.putAutomationRun(run) })
}

func (d *Daemon) runScheduled(a domain.Automation, repo string, due time.Time) {
	var live string
	d.query(func(s *state) {
		var newest domain.AutomationRun
		for _, r := range s.automationRuns {
			if r.Automation == a.Name && r.Repo == repo && r.SessionID != "" && r.Start.After(newest.Start) {
				newest = r
			}
		}
		if session, found := s.sessions[newest.SessionID]; found && !session.Ended {
			live = session.ID
		}
	})
	if live != "" && a.Session != domain.AutomationSessionReuse {
		d.recordScheduled(domain.AutomationRun{
			ID: newID(), Automation: a.Name, Trigger: domain.AutomationTriggerSchedule, Repo: repo,
			Start: d.ws.now(), End: d.ws.now(), Outcome: domain.AutomationSkipped, Reason: "the last run is still live in session " + live,
		})
		return
	}
	if _, _, rerr, _ := d.startAutomation(a, automationTrigger{kind: domain.AutomationTriggerSchedule, repo: repo}); rerr != nil {
		log.Printf("automation %s at %s: %s", a.Name, due.Format(time.RFC3339), rerr.Message)
	}
}
