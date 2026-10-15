package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const (
	mateSeenKept     = 512
	mateStatusEvery  = 5 * time.Second
	mateWorkItemMax  = 80
	mateStatusWorks  = "Working on it."
	mateStatusDone   = "Done."
	mateStatusFailed = "Stopped."
)

func (d *Daemon) firstSight(name string, e app.SlackEvent) bool {
	key := name + "\x00" + e.Channel + "\x00" + e.TS
	d.slack.mu.Lock()
	defer d.slack.mu.Unlock()
	for _, k := range d.slack.seen {
		if k == key {
			return false
		}
	}
	d.slack.seen = append(d.slack.seen, key)
	if len(d.slack.seen) > mateSeenKept {
		d.slack.seen = d.slack.seen[len(d.slack.seen)-mateSeenKept:]
	}
	return true
}

func (d *Daemon) findMate(name string) (domain.Mate, bool) {
	if d.cfg.path == "" {
		return domain.Mate{}, false
	}
	d.cfg.mu.Lock()
	src, err := readConfig(d.cfg.path)
	d.cfg.mu.Unlock()
	if err != nil {
		return domain.Mate{}, false
	}
	for _, m := range d.loadMates(src).Mates {
		if m.Name == name {
			return m, true
		}
	}
	return domain.Mate{}, false
}

func (d *Daemon) mateThreadEvent(ctx context.Context, name string, client app.SlackMate, e app.SlackEvent) {
	d.slack.threads.Lock()
	defer d.slack.threads.Unlock()
	key := domain.MateThreadKey(e.Team, e.Channel, e.ThreadTS, name)
	var link domain.MateThread
	var live bool
	if !d.query(func(s *state) {
		var found bool
		link, found = s.mateThreads[key]
		session, has := s.sessions[link.SessionID]
		live = found && has && !session.Ended
	}) {
		return
	}
	switch {
	case live:
		d.mateReply(ctx, client, link, e)
	case e.Kind == app.SlackMention:
		d.mateStart(ctx, name, client, e)
	}
}

func (d *Daemon) userName(ctx context.Context, client app.SlackMate, user string) string {
	if name, err := client.UserName(ctx, user); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return user
}

func (d *Daemon) mateReply(ctx context.Context, client app.SlackMate, link domain.MateThread, e app.SlackEvent) {
	asker := d.userName(ctx, client, e.User)
	if !d.query(func(s *state) {
		session := s.sessions[link.SessionID]
		if session.Mate == nil {
			return
		}
		authority := domain.NextMateAuthority(session.Mate.Authority, e.User, session.Mate.Owner, s.mateBusy(session))
		s.emit(MateTurn{SessionID: session.ID, User: authority})
	}) {
		return
	}
	resp, ok := d.sessionSend(rpc.Request{Method: rpc.MethodSessionSend, Params: mustJSON(rpc.SessionSendParams{Session: link.SessionID, Text: domain.MateReplyPrompt(asker, e.Text)})})
	if ok && resp.Error != nil {
		d.matePostError(ctx, client, e, "crew could not send your message to the session: "+resp.Error.Message)
	}
}

func (s *state) mateBusy(session domain.Session) bool {
	if session.State != domain.StateIdle && session.State != domain.StateDone {
		return true
	}
	if s.sendBusy(session.ID) {
		return true
	}
	for _, q := range s.sends {
		if q.Session == session.ID {
			return true
		}
	}
	return false
}

func (d *Daemon) mateStart(ctx context.Context, name string, client app.SlackMate, e app.SlackEvent) {
	m, ok := d.findMate(name)
	if !ok {
		return
	}
	var workspaces []domain.Workspace
	if !d.query(func(s *state) { workspaces = sorted(s.workspaces) }) {
		return
	}
	ws, ok := domain.MateWorkspace(m, workspaces)
	if !ok {
		d.matePostError(ctx, client, e, fmt.Sprintf("%s has no repo to work in. The owner must add a registered single repo to its repos.", name))
		return
	}
	asker := d.userName(ctx, client, e.User)
	prompt := domain.MateFirstPrompt(m, asker, d.threadLines(ctx, client, e, asker))
	session, rerr, ok := d.startSession(rpc.NewSessionParams{
		Workspace: ws.Root, WorkItem: mateWorkItem(name, e.Text), Harness: m.Agent, Model: m.Model, Effort: m.Effort, Prompt: prompt,
	}, "")
	switch {
	case !ok:
		return
	case rerr != nil:
		d.matePostError(ctx, client, e, "crew could not start a session: "+rerr.Message)
		return
	}
	link := domain.MateThread{Team: e.Team, Channel: e.Channel, ThreadTS: e.ThreadTS, Mate: name, SessionID: session.ID}
	if !d.query(func(s *state) {
		current, found := s.sessions[session.ID]
		if found {
			current.Mate = &domain.MateSeat{Name: name, Owner: m.Owner, Authority: e.User}
			s.emit(SessionChanged{Session: current})
		}
		s.putMateThread(link)
	}) {
		return
	}
	ts, err := client.PostMessage(ctx, e.Channel, e.ThreadTS, mateStatusWorks)
	if err != nil {
		log.Printf("[mates] %s: %v", name, err)
		return
	}
	d.query(func(s *state) {
		if cur, found := s.mateThreads[link.Key()]; found {
			cur.Status = ts
			s.putMateThread(cur)
		}
	})
}

func (s *state) putMateThread(t domain.MateThread) {
	s.mateThreads[t.Key()] = t
	s.store.PutMateThread(t)
}

func (d *Daemon) threadLines(ctx context.Context, client app.SlackMate, e app.SlackEvent, asker string) []domain.ThreadLine {
	msgs, err := client.Replies(ctx, e.Channel, e.ThreadTS)
	if err != nil || len(msgs) == 0 {
		return []domain.ThreadLine{{Author: asker, Text: e.Text}}
	}
	names := map[string]string{e.User: asker}
	lines := make([]domain.ThreadLine, 0, len(msgs))
	for _, m := range msgs {
		author := "a bot"
		if m.User != "" {
			if _, ok := names[m.User]; !ok {
				names[m.User] = d.userName(ctx, client, m.User)
			}
			author = names[m.User]
		}
		lines = append(lines, domain.ThreadLine{Author: author, Text: m.Text})
	}
	return lines
}

func mateWorkItem(name, text string) string {
	item := []rune(name + ": " + strings.Join(strings.Fields(text), " "))
	if len(item) > mateWorkItemMax {
		item = append(item[:mateWorkItemMax-1], '…')
	}
	return string(item)
}

func (d *Daemon) matePostError(ctx context.Context, client app.SlackMate, e app.SlackEvent, text string) {
	if _, err := client.PostMessage(ctx, e.Channel, e.ThreadTS, text); err != nil {
		log.Printf("[mates] %v", err)
	}
}

func (s *state) mateHooked(session domain.Session, ev domain.SessionEvent) {
	if session.Mate == nil || s.matePost == nil {
		return
	}
	switch ev.Kind {
	case domain.EventPreToolUse, domain.EventStop, domain.EventSessionEnd:
	default:
		return
	}
	for _, t := range s.mateThreads {
		if t.SessionID == session.ID {
			s.matePost(t, ev)
			return
		}
	}
}

func (d *Daemon) queueMatePost(link domain.MateThread, ev domain.SessionEvent) {
	if ev.Kind == domain.EventPreToolUse {
		now := time.Now()
		d.slack.mu.Lock()
		last, seen := d.slack.shown[link.SessionID]
		if seen && now.Sub(last) < mateStatusEvery {
			d.slack.mu.Unlock()
			return
		}
		d.slack.shown[link.SessionID] = now
		d.slack.mu.Unlock()
	}
	go d.matePostback(link, ev)
}

func (d *Daemon) mateClient(name string) app.SlackMate {
	d.slack.mu.Lock()
	defer d.slack.mu.Unlock()
	if w, ok := d.slack.running[name]; ok {
		return w.client
	}
	return nil
}

func (d *Daemon) matePostback(link domain.MateThread, ev domain.SessionEvent) {
	client := d.mateClient(link.Mate)
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ws.ctx, 30*time.Second)
	defer cancel()
	status := func(text string) {
		if link.Status == "" {
			return
		}
		if err := client.UpdateMessage(ctx, link.Channel, link.Status, text); err != nil {
			log.Printf("[mates] %s: %v", link.Mate, err)
		}
	}
	switch ev.Kind {
	case domain.EventPreToolUse:
		status(mateProgress(ev))
	case domain.EventStop:
		status(mateStatusDone)
		if _, err := client.PostMessage(ctx, link.Channel, link.ThreadTS, domain.MateResultText(ev.Text)); err != nil {
			log.Printf("[mates] %s: %v", link.Mate, err)
		}
	case domain.EventSessionEnd:
		status(mateStatusFailed)
	}
}

func mateProgress(ev domain.SessionEvent) string {
	step := strings.TrimSpace(ev.Tool + " " + domain.OneLine(ev.Detail, mateWorkItemMax))
	if step == "" {
		return mateStatusWorks
	}
	return mateStatusWorks + " Now: " + step
}
