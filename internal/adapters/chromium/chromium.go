package chromium

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var names = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome", "headless_shell", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"}

func Find(env func(string) string, look func(string) (string, error)) (string, bool) {
	if p := env("CREW_CHROMIUM"); p != "" {
		return p, true
	}
	for _, n := range names {
		if p, err := look(n); err == nil {
			return p, true
		}
	}
	return "", false
}

func InstallHint() string {
	return "install Chromium (Debian or Ubuntu: sudo apt install chromium) or the lighter chrome-headless-shell (npx @puppeteer/browsers install chrome-headless-shell), and set CREW_CHROMIUM to its binary if it is not on PATH"
}

type Browsers struct {
	Binary string
	Flags  []string
}

func (b Browsers) Open(ctx context.Context, session string) (app.HeadlessTab, error) {
	if b.Binary == "" {
		return nil, errors.New("no Chromium found: " + InstallHint())
	}
	dir, err := os.MkdirTemp("", "crew-chromium-")
	if err != nil {
		return nil, err
	}
	args := append([]string{
		"--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + dir,
		"--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-dev-shm-usage",
		"--disable-extensions", "--disable-background-networking", "--disable-sync", "--mute-audio",
		"--window-size=1280,800",
	}, b.Flags...)
	cmd := exec.Command(b.Binary, append(args, "about:blank")...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	stop := func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
		_ = os.RemoveAll(dir)
	}
	url, err := devtoolsURL(ctx, dir, exited)
	if err != nil {
		stop()
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	tab, err := connect(ctx, url)
	if err != nil {
		stop()
		return nil, err
	}
	tab.stop = stop
	return tab, nil
}

func devtoolsURL(ctx context.Context, dir string, exited <-chan struct{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) == 2 {
				return "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1]), nil
			}
		}
		select {
		case <-exited:
			return "", errors.New("chromium exited at start")
		case <-ctx.Done():
			return "", errors.New("chromium did not open its DevTools port")
		case <-tick.C:
		}
	}
}

type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

type reply struct {
	result json.RawMessage
	err    error
}

type Tab struct {
	conn    *websocket.Conn
	session string
	stop    func()
	mu      sync.Mutex
	next    int64
	pending map[int64]chan reply
	done    chan struct{}
	once    sync.Once
}

func connect(ctx context.Context, url string) (*Tab, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to chromium: %w", err)
	}
	conn.SetReadLimit(256 << 20)
	t := &Tab{conn: conn, pending: map[int64]chan reply{}, done: make(chan struct{})}
	go t.read()
	if err := t.attach(ctx); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Tab) attach(ctx context.Context) error {
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := t.call(ctx, "", "Target.getTargets", nil, &targets); err != nil {
		return err
	}
	target := ""
	for _, ti := range targets.TargetInfos {
		if ti.Type == "page" {
			target = ti.TargetID
			break
		}
	}
	if target == "" {
		var created struct {
			TargetID string `json:"targetId"`
		}
		if err := t.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
			return err
		}
		target = created.TargetID
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := t.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &attached); err != nil {
		return err
	}
	t.session = attached.SessionID
	var version struct {
		UserAgent string `json:"userAgent"`
	}
	if err := t.call(ctx, "", "Browser.getVersion", nil, &version); err != nil {
		return err
	}
	ua := strings.ReplaceAll(version.UserAgent, "HeadlessChrome", "Chrome")
	return t.call(ctx, t.session, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}, nil)
}

func (t *Tab) read() {
	defer t.failAll(errors.New("the headless browser closed"))
	for {
		_, data, err := t.conn.Read(context.Background())
		if err != nil {
			return
		}
		var m struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil || m.ID == 0 {
			continue
		}
		t.mu.Lock()
		ch, ok := t.pending[m.ID]
		delete(t.pending, m.ID)
		t.mu.Unlock()
		if !ok {
			continue
		}
		if m.Error != nil {
			ch <- reply{err: errors.New(m.Error.Message)}
		} else {
			ch <- reply{result: m.Result}
		}
	}
}

func (t *Tab) failAll(err error) {
	t.once.Do(func() { close(t.done) })
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, ch := range t.pending {
		ch <- reply{err: err}
		delete(t.pending, id)
	}
}

func (t *Tab) call(ctx context.Context, session, method string, params, out any) error {
	t.mu.Lock()
	t.next++
	id := t.next
	ch := make(chan reply, 1)
	t.pending[id] = ch
	t.mu.Unlock()
	msg := map[string]any{"id": id, "method": method}
	if session != "" {
		msg["sessionId"] = session
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := t.conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.forget(id)
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("%s: %w", method, r.err)
		}
		if out != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, out)
		}
		return nil
	case <-t.done:
		return errors.New("the headless browser closed")
	case <-ctx.Done():
		t.forget(id)
		return ctx.Err()
	}
}

func (t *Tab) forget(id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, id)
}

func (t *Tab) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if t.stop != nil {
		_ = t.call(ctx, "", "Browser.close", nil, nil)
	}
	err := t.conn.Close(websocket.StatusNormalClosure, "")
	if t.stop != nil {
		t.stop()
	}
	return err
}

func (t *Tab) SetCookies(ctx context.Context, cookies []domain.BrowserCookie) error {
	return t.call(ctx, "", "Storage.setCookies", map[string]any{"cookies": cookies}, nil)
}

func (t *Tab) Run(ctx context.Context, command string, raw json.RawMessage) (json.RawMessage, error) {
	var args map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
	}
	str := func(k string) string {
		s, _ := args[k].(string)
		return s
	}
	switch command {
	case "goto":
		return t.navigate(ctx, str("url"))
	case "snapshot":
		return t.script(ctx, scriptSnapshot, nil, false)
	case "click":
		return t.script(ctx, scriptClick, map[string]any{"ref": strings.TrimPrefix(str("ref"), "@")}, true)
	case "fill":
		return t.script(ctx, scriptFill, map[string]any{"ref": strings.TrimPrefix(str("ref"), "@"), "text": str("text")}, true)
	case "type":
		return t.script(ctx, scriptType, map[string]any{"text": str("text")}, false)
	case "press":
		return t.script(ctx, scriptPress, map[string]any{"key": str("key")}, true)
	case "eval":
		return t.script(ctx, scriptEval, map[string]any{"js": str("js")}, false)
	case "wait":
		return t.wait(ctx, args)
	case "screenshot":
		return t.screenshot(ctx)
	case "tabs":
		return t.tabs(ctx)
	case "cookies":
		return t.cookies(ctx, str("domain"))
	case "set-device":
		return t.device(ctx, str("device"))
	}
	return nil, fmt.Errorf("unknown browser command %q", command)
}

func (t *Tab) evaluate(ctx context.Context, expr string) (json.RawMessage, error) {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true, "userGesture": true}
	if err := t.call(ctx, t.session, "Runtime.evaluate", params, &res); err != nil {
		return nil, err
	}
	if e := res.ExceptionDetails; e != nil {
		msg := e.Exception.Description
		if msg == "" {
			msg = e.Text
		}
		msg, _, _ = strings.Cut(msg, "\n")
		return nil, errors.New(msg)
	}
	if len(res.Result.Value) == 0 {
		return json.RawMessage("null"), nil
	}
	return res.Result.Value, nil
}

func (t *Tab) script(ctx context.Context, body string, args map[string]any, settle bool) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	out, err := t.evaluate(ctx, "(async ({ref, text, key, js}) => {\n"+body+"\n})("+string(data)+")")
	if err == nil && settle {
		t.settle(ctx)
	}
	return out, err
}

func (t *Tab) settle(ctx context.Context) {
	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		v, err := t.evaluate(ctx, "document.readyState")
		if err == nil && string(v) == `"complete"` {
			return
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

func (t *Tab) navigate(ctx context.Context, url string) (json.RawMessage, error) {
	if url == "" {
		return nil, errors.New("goto needs a URL")
	}
	if !strings.Contains(url, "://") && !strings.HasPrefix(url, "about:") && !strings.HasPrefix(url, "data:") {
		url = "http://" + url
	}
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	if err := t.call(ctx, t.session, "Page.navigate", map[string]any{"url": url}, &nav); err != nil {
		return nil, err
	}
	if nav.ErrorText != "" {
		return nil, fmt.Errorf("goto %s: %s", url, nav.ErrorText)
	}
	t.settle(ctx)
	return t.page(ctx, "opened ")
}

func (t *Tab) page(ctx context.Context, prefix string) (json.RawMessage, error) {
	v, err := t.evaluate(ctx, "({url: location.href, title: document.title})")
	if err != nil {
		return nil, err
	}
	var p struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(v, &p)
	return json.Marshal(map[string]string{"text": prefix + p.URL, "url": p.URL, "title": p.Title})
}

func (t *Tab) wait(ctx context.Context, args map[string]any) (json.RawMessage, error) {
	if ms, ok := args["ms"].(float64); ok && ms >= 0 {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return json.Marshal(fmt.Sprintf("waited %d ms", int(ms)))
	}
	body, params := scriptHasText, map[string]any{"text": args["text"]}
	if ref, ok := args["ref"].(string); ok {
		body, params = scriptHasRef, map[string]any{"ref": strings.TrimPrefix(ref, "@")}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := t.script(ctx, body, params, false)
		if err == nil && string(v) == "true" {
			return json.Marshal("found")
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("timed out after 10 s")
}

func (t *Tab) screenshot(ctx context.Context) (json.RawMessage, error) {
	var shot struct {
		Data string `json:"data"`
	}
	if err := t.call(ctx, t.session, "Page.captureScreenshot", map[string]any{"format": "png"}, &shot); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, fmt.Errorf("the screenshot is not base64: %w", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("the screenshot is not a PNG: %w", err)
	}
	return json.Marshal(map[string]any{"png": shot.Data, "width": cfg.Width, "height": cfg.Height})
}

func (t *Tab) tabs(ctx context.Context) (json.RawMessage, error) {
	v, err := t.evaluate(ctx, "({url: location.href, title: document.title})")
	if err != nil {
		return nil, err
	}
	var p struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(v, &p)
	line := "* " + p.URL
	if p.Title != "" {
		line += "  " + p.Title
	}
	return json.Marshal(map[string]any{
		"text": line,
		"tabs": []map[string]any{{"id": "1", "url": p.URL, "title": p.Title, "selected": true}},
	})
}

func (t *Tab) cookies(ctx context.Context, domainName string) (json.RawMessage, error) {
	var res struct {
		Cookies []domain.BrowserCookie `json:"cookies"`
	}
	if err := t.call(ctx, "", "Storage.getCookies", nil, &res); err != nil {
		return nil, err
	}
	rows := []map[string]string{}
	var lines []string
	for _, c := range res.Cookies {
		host := strings.TrimPrefix(c.Domain, ".")
		if domainName != "" && host != domainName && !strings.HasSuffix(host, "."+domainName) && !strings.HasSuffix(domainName, "."+host) {
			continue
		}
		rows = append(rows, map[string]string{"name": c.Name, "domain": c.Domain, "path": c.Path})
		lines = append(lines, c.Domain+"\t"+c.Name)
	}
	text := strings.Join(lines, "\n")
	if text == "" {
		text = "no cookies"
	}
	return json.Marshal(map[string]any{"text": text, "cookies": rows})
}

type device struct {
	width, height int
	scale         float64
	mobile        bool
	userAgent     string
}

var devices = map[string]device{
	"desktop": {width: 1280, height: 800, scale: 1},
	"ipad":    {820, 1180, 2, true, "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"},
	"iphone":  {393, 852, 3, true, "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"},
}

func (t *Tab) device(ctx context.Context, name string) (json.RawMessage, error) {
	d, ok := devices[strings.ToLower(name)]
	if !ok {
		return nil, errors.New("set-device needs desktop, ipad or iphone")
	}
	metrics := map[string]any{"width": d.width, "height": d.height, "deviceScaleFactor": d.scale, "mobile": d.mobile}
	if err := t.call(ctx, t.session, "Emulation.setDeviceMetricsOverride", metrics, nil); err != nil {
		return nil, err
	}
	ua := d.userAgent
	if ua == "" {
		var version struct {
			UserAgent string `json:"userAgent"`
		}
		if err := t.call(ctx, "", "Browser.getVersion", nil, &version); err != nil {
			return nil, err
		}
		ua = strings.ReplaceAll(version.UserAgent, "HeadlessChrome", "Chrome")
	}
	if err := t.call(ctx, t.session, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}, nil); err != nil {
		return nil, err
	}
	if err := t.call(ctx, t.session, "Emulation.setTouchEmulationEnabled", map[string]any{"enabled": d.mobile}, nil); err != nil {
		return nil, err
	}
	return json.Marshal("device " + strings.ToLower(name))
}

func FromEnv() Browsers {
	bin, _ := Find(os.Getenv, exec.LookPath)
	return Browsers{Binary: bin, Flags: strings.Fields(os.Getenv("CREW_CHROMIUM_FLAGS"))}
}
