package daemon

import (
	"context"
	"log"
	"maps"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const automationEventBuffer = 256

func (s *state) automationEvents(evs []domain.AutomationEvent) {
	if s.autoEvents == nil {
		return
	}
	for _, ev := range evs {
		select {
		case s.autoEvents <- ev:
		default:
			log.Printf("automation event %s dropped: the queue is full", ev.Name)
		}
	}
}

func (s *state) sessionAutomationEvents(next domain.Session) {
	if s.autoEvents == nil {
		return
	}
	old, found := s.sessions[next.ID]
	if !found {
		return
	}
	task := s.tasks[next.TaskID]
	workItem := task.URL
	if workItem == "" {
		workItem = task.Text
	}
	repo := ""
	for _, id := range next.WorktreeIDs {
		if w, ok := s.worktrees[id]; ok && w.Repo != "" {
			repo = w.Repo
			break
		}
	}
	s.automationEvents(domain.SessionAutomationEvents(old, next, workItem, repo))
}

func (d *Daemon) runAutomationEvents(ctx context.Context, events <-chan domain.AutomationEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			d.handleAutomationEvent(ev)
		}
	}
}

func (d *Daemon) handleAutomationEvent(ev domain.AutomationEvent) {
	var trust map[string]string
	d.query(func(s *state) { trust = maps.Clone(s.automationTrust) })
	for _, a := range d.automations("").Automations {
		if !domain.AutomationCanRunAlone(a, trust) || !domain.MatchAutomationEvent(a, ev) {
			continue
		}
		repo := automationRepoOf(a)
		if repo == "" {
			repo = ev.Repo
		}
		if live := d.liveRunFor(a.Name, ev.Subject); live != "" {
			now := d.ws.now()
			d.recordScheduled(domain.AutomationRun{
				ID: newID(), Automation: a.Name, Trigger: domain.AutomationTriggerEvent, Repo: repo, Subject: ev.Subject,
				WorkItem: ev.WorkItem, Start: now, End: now, Outcome: domain.AutomationSkipped,
				Reason: "a run for " + ev.Subject + " is still live in session " + live,
			})
			continue
		}
		_, _, rerr, _ := d.startAutomation(a, automationTrigger{
			kind: domain.AutomationTriggerEvent, workItem: ev.WorkItem, repo: repo, event: ev.Text, subject: ev.Subject,
		})
		if rerr != nil {
			log.Printf("automation %s on %s: %s", a.Name, ev.Name, rerr.Message)
		}
	}
}

func (d *Daemon) liveRunFor(name, subject string) string {
	var live string
	d.query(func(s *state) {
		for _, r := range s.automationRuns {
			if r.Automation != name || r.Subject != subject || r.SessionID == "" {
				continue
			}
			if session, found := s.sessions[r.SessionID]; found && !session.Ended {
				live = session.ID
			}
		}
	})
	return live
}
