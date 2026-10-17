package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const DefaultHeadlessReap = 15 * time.Second

type headlessEntry struct {
	tab  app.HeadlessTab
	last time.Time
	busy int
}

type headlessPool struct {
	mu      sync.Mutex
	opener  app.HeadlessBrowsers
	cookies string
	reap    time.Duration
	cfg     domain.BrowserConfig
	tabs    map[string]*headlessEntry
	opening map[string]chan struct{}
	wants   map[string]string
}

func WithHeadlessBrowser(b app.HeadlessBrowsers, cookies string, reap time.Duration) Option {
	return func(d *Daemon) {
		d.headless.opener = b
		d.headless.cookies = cookies
		d.headless.reap = reap
	}
}

func LoadBrowserConfig(path string) (domain.BrowserConfig, error) {
	var cfg struct {
		Browser struct {
			Fallback    string `toml:"fallback"`
			MaxHeadless *int   `toml:"max_headless"`
			Idle        string `toml:"idle"`
		} `toml:"browser"`
	}
	if path == "" {
		return domain.DefaultBrowserConfig(), nil
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.DefaultBrowserConfig(), nil
	}
	if err != nil {
		return domain.DefaultBrowserConfig(), fmt.Errorf("browser: %s: %w", path, err)
	}
	return domain.ParseBrowserConfig(cfg.Browser.Fallback, cfg.Browser.MaxHeadless, cfg.Browser.Idle)
}

func (d *Daemon) loadBrowserConfig() {
	cfg, err := LoadBrowserConfig(d.cfg.path)
	if err != nil {
		log.Printf("[browser] %v", err)
	}
	d.headless.mu.Lock()
	d.headless.cfg = cfg
	d.headless.mu.Unlock()
}

func (p *headlessPool) config() domain.BrowserConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg == (domain.BrowserConfig{}) {
		return domain.DefaultBrowserConfig()
	}
	return p.cfg
}

func (p *headlessPool) want(session string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wants[session]
}

func (p *headlessPool) setWant(session, browser string) {
	if browser == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.wants == nil {
		p.wants = map[string]string{}
	}
	p.wants[session] = browser
}

func (p *headlessPool) acquire(ctx context.Context, session string, now time.Time) (*headlessEntry, error) {
	if p.opener == nil {
		return nil, errors.New("no browser: this daemon has no headless browser")
	}
	for {
		p.mu.Lock()
		if e, ok := p.tabs[session]; ok {
			e.busy++
			e.last = now
			p.mu.Unlock()
			return e, nil
		}
		if wait, ok := p.opening[session]; ok {
			p.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		running := make([]string, 0, len(p.tabs)+len(p.opening))
		for id := range p.tabs {
			running = append(running, id)
		}
		for id := range p.opening {
			running = append(running, id)
		}
		if err := domain.AdmitHeadless(running, session, p.cfgLocked().MaxHeadless); err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if p.opening == nil {
			p.opening = map[string]chan struct{}{}
		}
		done := make(chan struct{})
		p.opening[session] = done
		p.mu.Unlock()
		e, err := p.open(ctx, session, now)
		p.mu.Lock()
		delete(p.opening, session)
		close(done)
		if err == nil {
			if p.tabs == nil {
				p.tabs = map[string]*headlessEntry{}
			}
			p.tabs[session] = e
		}
		p.mu.Unlock()
		return e, err
	}
}

func (p *headlessPool) cfgLocked() domain.BrowserConfig {
	if p.cfg == (domain.BrowserConfig{}) {
		return domain.DefaultBrowserConfig()
	}
	return p.cfg
}

func (p *headlessPool) open(ctx context.Context, session string, now time.Time) (*headlessEntry, error) {
	tab, err := p.opener.Open(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("no browser: the headless browser did not start: %w", err)
	}
	cookies, err := p.loadCookies()
	if err == nil && len(cookies) > 0 {
		err = tab.SetCookies(ctx, cookies)
	}
	if err != nil {
		_ = tab.Close()
		return nil, fmt.Errorf("no browser: the headless browser did not take the cookies: %w", err)
	}
	return &headlessEntry{tab: tab, last: now, busy: 1}, nil
}

func (p *headlessPool) release(e *headlessEntry, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e.busy--
	e.last = now
}

func (p *headlessPool) loadCookies() ([]domain.BrowserCookie, error) {
	if p.cookies == "" {
		return nil, nil
	}
	data, err := os.ReadFile(p.cookies)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []domain.BrowserCookie
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", p.cookies, err)
	}
	return out, nil
}

func (p *headlessPool) importCookies(ctx context.Context, add []domain.BrowserCookie) (int, error) {
	total, tabs, err := p.keepCookies(add)
	if err != nil {
		return 0, err
	}
	for id, tab := range tabs {
		if err := tab.SetCookies(ctx, add); err != nil {
			log.Printf("[browser] the headless browser of %s refused the cookies, so it stops: %v", id, err)
			p.mu.Lock()
			if e, ok := p.tabs[id]; ok && e.tab == tab {
				delete(p.tabs, id)
			}
			p.mu.Unlock()
			_ = tab.Close()
		}
	}
	return total, nil
}

func (p *headlessPool) keepCookies(add []domain.BrowserCookie) (int, map[string]app.HeadlessTab, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	total, err := p.writeCookies(add)
	if err != nil {
		return 0, nil, err
	}
	tabs := map[string]app.HeadlessTab{}
	for id, e := range p.tabs {
		tabs[id] = e.tab
	}
	return total, tabs, nil
}

func (p *headlessPool) writeCookies(add []domain.BrowserCookie) (int, error) {
	if p.cookies == "" {
		return 0, errors.New("this daemon has no headless browser")
	}
	old, err := p.loadCookies()
	if err != nil {
		return 0, err
	}
	all := domain.MergeCookies(old, add)
	data, err := json.Marshal(all)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p.cookies), 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.cookies), ".cookies-*.json")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), p.cookies); err != nil {
		return 0, err
	}
	return len(all), nil
}

func (p *headlessPool) stopIdle(live map[string]bool, now time.Time) {
	p.mu.Lock()
	last := map[string]time.Time{}
	for id, e := range p.tabs {
		if e.busy == 0 {
			last[id] = e.last
		}
	}
	var stop []app.HeadlessTab
	for _, id := range domain.HeadlessToStop(last, live, now, p.cfgLocked().Idle) {
		stop = append(stop, p.tabs[id].tab)
		delete(p.tabs, id)
		if !live[id] {
			delete(p.wants, id)
		}
	}
	p.mu.Unlock()
	for _, tab := range stop {
		_ = tab.Close()
	}
}

func (p *headlessPool) stopAll() {
	p.mu.Lock()
	tabs := make([]app.HeadlessTab, 0, len(p.tabs))
	for _, e := range p.tabs {
		tabs = append(tabs, e.tab)
	}
	p.tabs = nil
	p.mu.Unlock()
	for _, tab := range tabs {
		_ = tab.Close()
	}
}

func (d *Daemon) reapHeadless(ctx context.Context) {
	every := d.headless.reap
	if every <= 0 {
		every = DefaultHeadlessReap
	}
	t := time.NewTicker(every)
	defer t.Stop()
	defer d.headless.stopAll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		live := map[string]bool{}
		if !d.query(func(s *state) {
			for id, sess := range s.sessions {
				if !sess.Ended {
					live[id] = true
				}
			}
		}) {
			return
		}
		d.headless.stopIdle(live, d.ws.now())
	}
}

func (d *Daemon) headlessCommand(req rpc.Request, session, command string, args json.RawMessage, timeout time.Duration) *rpc.Response {
	ctx, cancel := context.WithTimeout(d.ws.ctx, timeout)
	defer cancel()
	e, err := d.headless.acquire(ctx, session, d.ws.now())
	if err != nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, err.Error())
	}
	defer d.headless.release(e, d.ws.now())
	out, err := e.tab.Run(ctx, command, args)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return errorResponse(req.ID, rpc.CodeFailed, fmt.Sprintf("browser: no reply from the headless browser in %v", timeout))
	case err != nil:
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	case len(out) == 0:
		return result(req.ID, struct{}{})
	}
	return &rpc.Response{V: rpc.Version, ID: req.ID, Result: out}
}

func (d *Daemon) browserCookiesImport(req rpc.Request) *rpc.Response {
	var p rpc.BrowserCookiesParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.cookies.import params: "+err.Error())
	}
	if len(p.Cookies) == 0 {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.cookies.import needs cookies")
	}
	if slices.ContainsFunc(p.Cookies, func(c domain.BrowserCookie) bool { return c.Name == "" || c.Domain == "" }) {
		return errorResponse(req.ID, rpc.CodeBadRequest, "each cookie needs a name and a domain")
	}
	ctx, cancel := context.WithTimeout(d.ws.ctx, browserTimeout)
	defer cancel()
	total, err := d.headless.importCookies(ctx, p.Cookies)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	return result(req.ID, rpc.BrowserCookiesResult{Imported: len(p.Cookies), Total: total})
}

func (p *headlessPool) peek(session string) (*headlessEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.tabs[session]
	if ok {
		e.busy++
	}
	return e, ok
}

func (p *headlessPool) unpeek(e *headlessEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e.busy--
}

func (d *Daemon) browserHeadlessView(req rpc.Request) *rpc.Response {
	var p rpc.BrowserHeadlessViewParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.headless.view params: "+err.Error())
	}
	if p.Session == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "browser.headless.view needs a session")
	}
	e, ok := d.headless.peek(p.Session)
	if !ok {
		return result(req.ID, rpc.BrowserHeadlessView{})
	}
	defer d.headless.unpeek(e)
	ctx, cancel := context.WithTimeout(d.ws.ctx, browserTimeout)
	defer cancel()
	out, err := e.tab.Run(ctx, "screenshot", json.RawMessage("{}"))
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	var view rpc.BrowserHeadlessView
	if err := json.Unmarshal(out, &view); err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, "the headless screenshot: "+err.Error())
	}
	view.Live = true
	return result(req.ID, view)
}
