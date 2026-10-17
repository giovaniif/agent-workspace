package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const mateSyncEvery = 30 * time.Second

type slackWorkers struct {
	connect app.SlackConnector
	every   time.Duration
	kick    chan struct{}
	mu      sync.Mutex
	states  map[string]rpc.MateState
	running map[string]*mateWorker
	claims  map[string]domain.MateClaim
	seen    []string
	threads sync.Mutex
	shown   map[string]time.Time
	users   map[string]string
	guarded map[string]domain.MateGuardStop
	limits  domain.MateLimits
}

type mateWorker struct {
	print  string
	cancel context.CancelFunc
	client app.SlackMate
}

type mateTarget struct {
	print, bot, app string
}

func WithSlack(connect app.SlackConnector, every time.Duration) Option {
	return func(d *Daemon) {
		d.slack.connect = connect
		d.slack.every = every
		d.slack.kick = make(chan struct{}, 1)
		d.slack.states = map[string]rpc.MateState{}
		d.slack.running = map[string]*mateWorker{}
		d.slack.claims = map[string]domain.MateClaim{}
		d.slack.shown = map[string]time.Time{}
		d.slack.users = map[string]string{}
		d.slack.guarded = map[string]domain.MateGuardStop{}
		d.st.matePost = d.queueMatePost
		d.st.approvalAsk = func(a domain.Approval) { go d.askApproval(a) }
	}
}

func (d *Daemon) kickMates() {
	if d.slack.kick == nil {
		return
	}
	select {
	case d.slack.kick <- struct{}{}:
	default:
	}
}

func (d *Daemon) runMates(ctx context.Context) {
	tick := time.NewTicker(d.slack.every)
	defer tick.Stop()
	for {
		d.syncMates(ctx)
		d.publishMates()
		select {
		case <-ctx.Done():
			d.slack.mu.Lock()
			for name, w := range d.slack.running {
				w.cancel()
				delete(d.slack.running, name)
			}
			d.slack.mu.Unlock()
			return
		case <-d.slack.kick:
		case <-tick.C:
		}
	}
}

func tokenPrint(bot, appToken string) string {
	sum := sha256.Sum256([]byte(bot + "\n" + appToken))
	return hex.EncodeToString(sum[:])
}

func (d *Daemon) syncMates(ctx context.Context) {
	if d.cfg.path == "" {
		return
	}
	d.cfg.mu.Lock()
	src, err := readConfig(d.cfg.path)
	d.cfg.mu.Unlock()
	if err != nil {
		log.Printf("[mates] %v", err)
		return
	}
	reader, _ := d.mateSecrets.(app.MateTokenReader)
	states := map[string]rpc.MateState{}
	want := map[string]mateTarget{}
	list := d.loadMates(src)
	for _, m := range list.Mates {
		if !m.Enabled {
			states[m.Name] = rpc.MateState{Name: m.Name, State: domain.MatePaused}
			continue
		}
		if reader == nil {
			states[m.Name] = rpc.MateState{Name: m.Name, State: domain.MateNoTokens}
			continue
		}
		bot, appToken, err := reader.MateTokens(m.Name)
		if err != nil {
			states[m.Name] = rpc.MateState{Name: m.Name, State: domain.MateNoTokens}
			continue
		}
		want[m.Name] = mateTarget{print: tokenPrint(bot, appToken), bot: bot, app: appToken}
	}
	d.slack.mu.Lock()
	defer d.slack.mu.Unlock()
	for name, w := range d.slack.running {
		if t, ok := want[name]; !ok || t.print != w.print {
			w.cancel()
			delete(d.slack.running, name)
			delete(d.slack.users, name)
		}
	}
	for name, t := range want {
		if _, ok := d.slack.running[name]; ok {
			states[name] = d.slack.states[name]
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		w := &mateWorker{print: t.print, cancel: cancel, client: d.slack.connect(t.bot, t.app)}
		d.slack.running[name] = w
		states[name] = rpc.MateState{Name: name, State: domain.MateConnecting}
		go d.runMate(wctx, name, w)
	}
	d.slack.states = states
	d.slack.limits = list.Limits
}

func (d *Daemon) publishMates() {
	d.query(func(s *state) {
		d.slack.mu.Lock()
		states := slices.Collect(maps.Values(d.slack.states))
		limits := d.slack.limits
		d.slack.mu.Unlock()
		board := domain.NewMateBoard(states, limits, s.mateUsage, d.ws.now())
		if !reflect.DeepEqual(board, s.mateBoard) {
			s.emit(MatesChanged{Board: board})
		}
	})
}

type MatesChanged struct{ Board domain.MateBoard }

func (e MatesChanged) apply(s *state) rpc.Diff {
	s.mateBoard = e.Board
	board := s.mateBoardCopy()
	return rpc.Diff{Mates: &board}
}

func (s *state) mateBoardCopy() domain.MateBoard {
	return domain.MateBoard{Stopped: s.mateBoard.Stopped, Mates: append([]domain.MateStatus{}, s.mateBoard.Mates...)}
}

func (d *Daemon) runMate(ctx context.Context, name string, w *mateWorker) {
	err := w.client.Run(ctx,
		func(e app.SlackEvent) { d.slackEvent(ctx, name, w.client, e) },
		func(s app.SlackStatus) { d.setMateState(name, w, s) },
	)
	if ctx.Err() != nil || err == nil {
		return
	}
	log.Printf("[mates] %s: slack stopped: %v", name, err)
	d.slack.mu.Lock()
	if d.slack.running[name] == w && d.slack.states[name].State != domain.MateError {
		d.slack.states[name] = rpc.MateState{Name: name, State: domain.MateError, Error: err.Error()}
	}
	d.slack.mu.Unlock()
	d.publishMates()
}

func (d *Daemon) setMateState(name string, w *mateWorker, s app.SlackStatus) {
	d.slack.mu.Lock()
	if d.slack.running[name] == w {
		d.slack.states[name] = rpc.MateState{Name: name, State: s.State, Error: s.Error}
		if s.User != "" {
			d.slack.users[name] = s.User
		}
	}
	d.slack.mu.Unlock()
	d.publishMates()
}

func (d *Daemon) mateStates(mates []domain.Mate, limits domain.MateLimits) []rpc.MateState {
	if d.slack.connect == nil {
		return nil
	}
	usage := map[string]domain.MateUsage{}
	d.query(func(s *state) { maps.Copy(usage, s.mateUsage) })
	now := d.ws.now()
	d.slack.mu.Lock()
	defer d.slack.mu.Unlock()
	var out []rpc.MateState
	for _, m := range mates {
		if s, ok := d.slack.states[m.Name]; ok {
			s.Guard = domain.MateGuard(limits, 0, usage[m.Name], now)
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) mateClaim(req rpc.Request) *rpc.Response {
	if d.slack.connect == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no Slack worker")
	}
	var p rpc.MateClaimParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "mate.claim needs a name")
	}
	if !d.hasMate(p.Name) {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no mate %q", p.Name))
	}
	c := domain.NewMateClaim(p.Name, domain.NewPairCode(randomIntn), d.ws.now())
	d.slack.mu.Lock()
	d.slack.claims[p.Name] = c
	d.slack.mu.Unlock()
	return result(req.ID, rpc.MateClaim{Name: p.Name, Code: c.Code, ExpiresAt: c.Expires})
}

func (d *Daemon) slackEvent(ctx context.Context, name string, client app.SlackMate, e app.SlackEvent) {
	switch e.Kind {
	case app.SlackMention, app.SlackThreadMessage:
		if d.firstSight(name, e) {
			go d.mateThreadEvent(ctx, name, client, e)
		}
		return
	case app.SlackAction:
		go d.slackApproval(name, e)
		return
	case app.SlackDirectMessage:
	default:
		return
	}
	d.slack.mu.Lock()
	c, ok := d.slack.claims[name]
	if !ok || !c.Accepts(e.User, e.Text, d.ws.now()) {
		d.slack.mu.Unlock()
		return
	}
	delete(d.slack.claims, name)
	d.slack.mu.Unlock()
	go d.finishClaim(ctx, name, client, e)
}

func (d *Daemon) finishClaim(ctx context.Context, name string, client app.SlackMate, e app.SlackEvent) {
	reply := fmt.Sprintf("You are now the owner of %s.", name)
	if err := d.setMateOwner(name, e.User); err != nil {
		log.Printf("[mates] %s: the owner is not saved: %v", name, err)
		reply = fmt.Sprintf("crew could not save you as the owner of %s. Run crew mate claim %s again.", name, name)
	}
	if _, err := client.PostMessage(ctx, e.Channel, "", reply); err != nil {
		log.Printf("[mates] %s: %v", name, err)
	}
}

func (d *Daemon) setMateOwner(name, user string) error {
	d.cfg.mu.Lock()
	defer d.cfg.mu.Unlock()
	src, failed := d.editableConfig(0)
	if failed != nil {
		return errors.New(failed.Error.Message)
	}
	parsed, _ := parseMates(src, d.cfg.path)
	for _, m := range parsed {
		if m.Name != name {
			continue
		}
		m.Owner = user
		next := domain.SetMateBlock(src, m)
		if next == src {
			return nil
		}
		return d.replaceConfig(src, next)
	}
	return fmt.Errorf("no mate %q", name)
}
