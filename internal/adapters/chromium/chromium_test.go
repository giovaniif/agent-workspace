package chromium

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/crew/internal/domain"
)

func TestFindPrefersCrewChromiumThenTheKnownNames(t *testing.T) {
	env := func(k string) string {
		if k == "CREW_CHROMIUM" {
			return "/opt/chrome/chrome"
		}
		return ""
	}
	none := func(string) (string, error) { return "", errors.New("not found") }
	if got, ok := Find(env, none); !ok || got != "/opt/chrome/chrome" {
		t.Fatalf("got %q %v", got, ok)
	}
	look := func(name string) (string, error) {
		if name == "google-chrome" {
			return "/usr/bin/google-chrome", nil
		}
		return "", errors.New("not found")
	}
	empty := func(string) string { return "" }
	if got, ok := Find(empty, look); !ok || got != "/usr/bin/google-chrome" {
		t.Fatalf("got %q %v", got, ok)
	}
	if got, ok := Find(empty, none); ok {
		t.Fatalf("found %q with nothing installed", got)
	}
}

func TestInstallHintNamesAPackageAndCrewChromium(t *testing.T) {
	hint := InstallHint()
	for _, want := range []string{"chromium", "CREW_CHROMIUM"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q does not name %s", hint, want)
		}
	}
}

type cdpMessage struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
}

type fakeCDP struct {
	mu     sync.Mutex
	got    []cdpMessage
	answer func(m cdpMessage) (any, *string)
}

func (f *fakeCDP) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.got {
		out = append(out, m.Method)
	}
	return out
}

func (f *fakeCDP) last(method string) cdpMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.got) - 1; i >= 0; i-- {
		if f.got[i].Method == method {
			return f.got[i]
		}
	}
	return cdpMessage{}
}

func (f *fakeCDP) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m cdpMessage
			if json.Unmarshal(data, &m) != nil {
				return
			}
			f.mu.Lock()
			f.got = append(f.got, m)
			f.mu.Unlock()
			res, msg := f.base(m)
			if f.answer != nil {
				if r, e := f.answer(m); r != nil || e != nil {
					res, msg = r, e
				}
			}
			out := map[string]any{"id": m.ID, "sessionId": m.SessionID}
			if msg != nil {
				out["error"] = map[string]any{"code": -32000, "message": *msg}
			} else {
				out["result"] = res
			}
			b, _ := json.Marshal(out)
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"method":"Page.loadEventFired","params":{}}`))
			if err := c.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func (f *fakeCDP) base(m cdpMessage) (any, *string) {
	switch m.Method {
	case "Target.getTargets":
		return map[string]any{"targetInfos": []map[string]any{{"targetId": "T1", "type": "page", "url": "about:blank"}}}, nil
	case "Target.attachToTarget":
		return map[string]any{"sessionId": "S1"}, nil
	case "Browser.getVersion":
		return map[string]any{"userAgent": "Mozilla/5.0 HeadlessChrome/140.0"}, nil
	case "Runtime.evaluate":
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if strings.Contains(p.Expression, "document.readyState") {
			return map[string]any{"result": map[string]any{"type": "string", "value": "complete"}}, nil
		}
	}
	return map[string]any{}, nil
}

func connectFake(t *testing.T, f *fakeCDP) *Tab {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tab, err := connect(ctx, f.serve(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tab.Close() })
	return tab
}

func run(t *testing.T, tab *Tab, command string, args map[string]any) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return tab.Run(ctx, command, raw)
}

func TestConnectAttachesToThePageAndHidesHeadlessInTheUserAgent(t *testing.T) {
	f := &fakeCDP{}
	connectFake(t, f)
	ua := f.last("Emulation.setUserAgentOverride")
	if ua.SessionID != "S1" || !strings.Contains(string(ua.Params), "Chrome/140.0") || strings.Contains(string(ua.Params), "Headless") {
		t.Fatalf("user agent %+v", ua)
	}
}

func TestSnapshotRunsTheSharedScriptAndReturnsItsValue(t *testing.T) {
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Runtime.evaluate" && strings.Contains(string(m.Params), "data-crew-ref") {
			return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"text": "# t\n- button \"Go\" [@e1]", "refs": 1}}}, nil
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	out, err := run(t, tab, "snapshot", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &got); err != nil || !strings.Contains(got.Text, "[@e1]") {
		t.Fatalf("got %s: %v", out, err)
	}
	ev := f.last("Runtime.evaluate")
	if ev.SessionID != "S1" || !strings.Contains(string(ev.Params), `"awaitPromise":true`) {
		t.Fatalf("evaluate %+v", ev)
	}
}

func TestClickPassesTheRefWithoutTheAtSign(t *testing.T) {
	f := &fakeCDP{}
	tab := connectFake(t, f)
	if _, err := run(t, tab, "click", map[string]any{"ref": "@e7"}); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Expression string `json:"expression"`
	}
	f.mu.Lock()
	for _, m := range f.got {
		if m.Method == "Runtime.evaluate" && strings.Contains(string(m.Params), "clicked") {
			_ = json.Unmarshal(m.Params, &p)
		}
	}
	f.mu.Unlock()
	if !strings.Contains(p.Expression, `"ref":"e7"`) {
		t.Fatalf("expression %s", p.Expression)
	}
}

func TestAScriptExceptionIsTheError(t *testing.T) {
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Runtime.evaluate" && strings.Contains(string(m.Params), "nope") {
			return map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "ReferenceError: nope is not defined\n    at <anonymous>:1:1"}}}, nil
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	_, err := run(t, tab, "eval", map[string]any{"js": "nope"})
	if err == nil || err.Error() != "ReferenceError: nope is not defined" {
		t.Fatalf("got %v", err)
	}
}

func TestGotoFailsWithTheNavigationError(t *testing.T) {
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Page.navigate" {
			return map[string]any{"errorText": "net::ERR_NAME_NOT_RESOLVED"}, nil
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	_, err := run(t, tab, "goto", map[string]any{"url": "nowhere.invalid"})
	if err == nil || !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(string(f.last("Page.navigate").Params), `"url":"http://nowhere.invalid"`) {
		t.Fatalf("navigate %s", f.last("Page.navigate").Params)
	}
}

func TestScreenshotReturnsThePNGAndItsSize(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Page.captureScreenshot" {
			return map[string]any{"data": data}, nil
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	out, err := run(t, tab, "screenshot", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		PNG    string `json:"png"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal(out, &got); err != nil || got.PNG != data || got.Width != 4 || got.Height != 3 {
		t.Fatalf("got %+v: %v", got, err)
	}
}

func TestCookiesShowNamesAndDomainsButNoValues(t *testing.T) {
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Storage.getCookies" {
			return map[string]any{"cookies": []map[string]any{
				{"name": "sid", "value": "secret", "domain": ".example.com", "path": "/"},
				{"name": "x", "value": "secret", "domain": "other.org", "path": "/"},
			}}, nil
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	out, err := run(t, tab, "cookies", map[string]any{"domain": "app.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "secret") || !strings.Contains(string(out), "sid") || strings.Contains(string(out), "other.org") {
		t.Fatalf("got %s", out)
	}
}

func TestSetCookiesSendsThemToTheBrowser(t *testing.T) {
	f := &fakeCDP{}
	tab := connectFake(t, f)
	err := tab.SetCookies(context.Background(), []domain.BrowserCookie{{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/", HTTPOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	p := string(f.last("Storage.setCookies").Params)
	if !strings.Contains(p, `"name":"sid"`) || !strings.Contains(p, `"httpOnly":true`) {
		t.Fatalf("params %s", p)
	}
}

func TestSetDeviceEmulatesTheIPhone(t *testing.T) {
	f := &fakeCDP{}
	tab := connectFake(t, f)
	out, err := run(t, tab, "set-device", map[string]any{"device": "iphone"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"device iphone"` {
		t.Fatalf("got %s", out)
	}
	m := string(f.last("Emulation.setDeviceMetricsOverride").Params)
	if !strings.Contains(m, `"width":393`) || !strings.Contains(m, `"mobile":true`) {
		t.Fatalf("metrics %s", m)
	}
}

func TestAnErrorReplyOfTheBrowserIsTheError(t *testing.T) {
	msg := "Cannot navigate to invalid URL"
	f := &fakeCDP{answer: func(m cdpMessage) (any, *string) {
		if m.Method == "Page.navigate" {
			return nil, &msg
		}
		return nil, nil
	}}
	tab := connectFake(t, f)
	_, err := run(t, tab, "goto", map[string]any{"url": "http://x/"})
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Fatalf("got %v", err)
	}
	if !contains(f.methods(), "Page.navigate") {
		t.Fatal("no navigate")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
