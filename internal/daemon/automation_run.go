package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const precheckTimeout = 2 * time.Minute

type automationCfg struct {
	precheck      app.CommandRunner
	scheduleEvery time.Duration
}

func WithAutomationPrecheck(runner app.CommandRunner) Option {
	return func(d *Daemon) { d.auto.precheck = runner }
}

type automationTrigger struct {
	kind     string
	context  string
	workItem string
	repo     string
	event    string
	subject  string
}

func (d *Daemon) automationRun(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.AutomationRunParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "automation.run params: "+err.Error()), true
	}
	if p.Rerun != "" {
		var prev domain.AutomationRun
		var found bool
		if !d.query(func(s *state) { prev, found = s.automationRuns[p.Rerun] }) {
			return nil, false
		}
		if !found {
			return errorResponse(req.ID, rpc.CodeNotFound, "no automation run "+p.Rerun), true
		}
		p = rpc.AutomationRunParams{Name: prev.Automation, Context: prev.Context, WorkItem: prev.WorkItem, Repo: prev.Repo}
	}
	if strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "automation.run needs a name"), true
	}
	a, found := d.findAutomation(p.Name, p.Repo)
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no automation %q", p.Name)), true
	}
	run, session, rerr, ok := d.startAutomation(a, automationTrigger{
		kind: domain.AutomationTriggerManual, context: p.Context, workItem: p.WorkItem, repo: p.Repo,
	})
	switch {
	case !ok:
		return nil, false
	case rerr != nil:
		return errorResponse(req.ID, rerr.Code, rerr.Message), true
	}
	return result(req.ID, rpc.AutomationRunResult{Run: run, Session: session}), true
}

func (d *Daemon) findAutomation(name, repo string) (domain.Automation, bool) {
	for _, a := range d.automations(repo).Automations {
		if a.Name == name {
			return a, true
		}
	}
	return domain.Automation{}, false
}

func (d *Daemon) automationRepo(a domain.Automation, repo string) string {
	if repo != "" {
		return filepath.Clean(repo)
	}
	if filepath.Base(a.Source) == repoConfigFile {
		return filepath.Dir(a.Source)
	}
	var root string
	d.query(func(s *state) {
		if ws, found := domain.LastUsedWorkspace(sorted(s.workspaces)); found {
			root = ws.Root
		}
	})
	return root
}

func (d *Daemon) startAutomation(a domain.Automation, t automationTrigger) (domain.AutomationRun, domain.Session, *rpc.Error, bool) {
	repo := d.automationRepo(a, t.repo)
	now := d.ws.now()
	var in domain.AutomationRunInput
	if !d.query(func(s *state) {
		in = domain.AutomationRunInput{Runs: slices.Collect(maps.Values(s.automationRuns)), Live: map[string]bool{}}
		for id, sess := range s.sessions {
			if !sess.Ended {
				in.Live[id] = true
			}
		}
	}) {
		return domain.AutomationRun{}, domain.Session{}, nil, false
	}
	in.Context, in.WorkItem, in.Repo, in.Event, in.Now = t.context, t.workItem, repo, t.event, now
	plan := domain.PlanAutomationRun(a, in)
	run := domain.AutomationRun{
		ID: newID(), Automation: a.Name, Trigger: t.kind, Context: t.context, WorkItem: t.workItem,
		Repo: t.repo, Subject: t.subject, Start: now,
	}
	var session domain.Session
	if reason := d.precheck(a, plan, repo); reason != "" {
		run.Outcome, run.Reason = domain.AutomationSkipped, reason
	} else {
		var rerr *rpc.Error
		var ok bool
		session, rerr, ok = d.startPlanned(a, plan)
		if !ok {
			return domain.AutomationRun{}, domain.Session{}, nil, false
		}
		if rerr != nil {
			run.Outcome, run.Reason = domain.AutomationFailed, rerr.Message
		} else {
			run.Outcome, run.SessionID = domain.AutomationSucceeded, session.ID
		}
	}
	run.End = d.ws.now()
	if !d.query(func(s *state) { s.putAutomationRun(run) }) {
		return domain.AutomationRun{}, domain.Session{}, nil, false
	}
	return run, session, nil, true
}

func (s *state) putAutomationRun(run domain.AutomationRun) {
	s.automationRuns[run.ID] = run
	s.store.PutAutomationRun(run)
}

func (d *Daemon) precheck(a domain.Automation, plan domain.AutomationStart, repo string) string {
	if strings.TrimSpace(a.Precheck) == "" {
		return ""
	}
	if d.auto.precheck == nil {
		return "this daemon cannot run a precheck"
	}
	dir := repo
	if plan.Kind == domain.AutomationStartLaunch {
		dir = plan.Dir
	}
	ctx, cancel := context.WithTimeout(d.ws.ctx, precheckTimeout)
	defer cancel()
	if err := d.auto.precheck.Run(ctx, dir, "sh", "-c", a.Precheck); err != nil {
		return "precheck failed: " + err.Error()
	}
	return ""
}

func (d *Daemon) startPlanned(a domain.Automation, plan domain.AutomationStart) (domain.Session, *rpc.Error, bool) {
	switch plan.Kind {
	case domain.AutomationStartSend:
		resp, ok := d.sessionSend(rpc.Request{Method: rpc.MethodSessionSend, Params: mustJSON(rpc.SessionSendParams{Session: plan.Session, Text: plan.Prompt})})
		if !ok {
			return domain.Session{}, nil, false
		}
		if resp.Error != nil {
			return domain.Session{}, resp.Error, true
		}
		var session domain.Session
		ok = d.query(func(s *state) { session = s.sessions[plan.Session] })
		return session, nil, ok
	case domain.AutomationStartLaunch:
		return d.launchSession(rpc.LaunchParams{Harness: a.Agent, Dir: plan.Dir, Model: a.Model, Effort: a.Effort, Name: plan.WorkItem, Prompt: plan.Prompt})
	default:
		return d.startSession(rpc.NewSessionParams{Workspace: plan.Workspace, WorkItem: plan.WorkItem, Harness: a.Agent, Model: a.Model, Effort: a.Effort, Prompt: plan.Prompt}, "")
	}
}

func (d *Daemon) automationRunsMethod(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.AutomationRunsParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "automation.runs params: "+err.Error()), true
		}
	}
	var runs []domain.AutomationRun
	ok := d.query(func(s *state) {
		runs = domain.AutomationRunsOf(slices.Collect(maps.Values(s.automationRuns)), p.Name, p.Limit)
	})
	if runs == nil {
		runs = []domain.AutomationRun{}
	}
	return result(req.ID, rpc.AutomationRuns{Runs: runs}), ok
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
