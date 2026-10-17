package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type headlessRig struct {
	d       *daemon.Daemon
	path    string
	c       *rpc.Client
	b       *fakeHeadless
	cookies string
}

func startHeadless(t *testing.T, config string, reap time.Duration) headlessRig {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	r := headlessRig{b: &fakeHeadless{}, cookies: filepath.Join(dir, "browser", "profiles", "default", "cookies.json")}
	r.d, r.path = start(t, &memStore{}, daemon.WithConfig(cfg), daemon.WithHeadlessBrowser(r.b, r.cookies, reap))
	r.c = dial(t, r.path)
	return r
}

const headlessOn = "[browser]\nfallback = \"headless\"\n"

func TestBrowserCommandWithoutTheMacAppUsesTheHeadlessFallback(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	out, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "goto", Args: map[string]any{"url": "http://devbox:3000/"}})
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil || res["text"] != "headless goto" || res["session"] != "s1" {
		t.Fatalf("result %s: %v", out, err)
	}
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if r.b.openCount() != 1 {
		t.Fatalf("opened %d browsers for one session", r.b.openCount())
	}
	if got := r.b.tab("s1").calls; !reflect.DeepEqual(got, []string{`goto {"url":"http://devbox:3000/"}`, "snapshot {}"}) {
		t.Fatalf("calls %q", got)
	}
}

func TestHeadlessBrowserErrorsComeBackAsFailed(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	_, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "eval", Args: map[string]any{"js": "nope"}})
	if errCode(err) != rpc.CodeFailed || !strings.Contains(rpcMessage(err), "ReferenceError") {
		t.Fatalf("got %v", err)
	}
}

func TestHeadlessFallbackIsOffByDefault(t *testing.T) {
	r := startHeadless(t, "", time.Hour)
	_, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"})
	if errCode(err) != rpc.CodeUnavailable || rpcMessage(err) != rpc.NoBrowser {
		t.Fatalf("got %v", err)
	}
	if r.b.openCount() != 0 {
		t.Fatal("started a headless browser with the fallback off")
	}
}

func TestTheConnectedMacAppWinsOverTheHeadlessFallback(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	app := attachBrowser(t, r.path)
	done := make(chan error, 1)
	go func() {
		_, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"})
		done <- err
	}()
	call := app.call()
	app.reply(call.Call, "mac", "")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.b.openCount() != 0 {
		t.Fatal("started a headless browser while the Mac app is connected")
	}
}

func TestHeadlessBrowsersStayUnderMaxHeadless(t *testing.T) {
	r := startHeadless(t, headlessOn+"max_headless = 1\n", time.Hour)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	_, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s2", Command: "snapshot"})
	if errCode(err) != rpc.CodeUnavailable || !strings.Contains(rpcMessage(err), "max_headless") {
		t.Fatalf("got %v", err)
	}
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "tabs"}); err != nil {
		t.Fatalf("the session that has the browser: %v", err)
	}
}

func TestAnIdleHeadlessBrowserStopsAndTheNextCommandStartsANewOne(t *testing.T) {
	r := startHeadless(t, headlessOn+"idle = \"50ms\"\n", 10*time.Millisecond)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s1", Harness: domain.HarnessClaude, State: domain.StateRunning}})
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	first := r.b.tab("s1")
	waitUntil(t, "the idle browser stops", first.isClosed)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if r.b.openCount() != 2 {
		t.Fatalf("opened %d, want a new browser after the idle stop", r.b.openCount())
	}
}

func TestTheHeadlessBrowserOfAnEndedSessionStops(t *testing.T) {
	r := startHeadless(t, headlessOn, 10*time.Millisecond)
	s := domain.Session{ID: "s1", Harness: domain.HarnessClaude, State: domain.StateRunning}
	r.d.Post(daemon.SessionChanged{Session: s})
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	tab := r.b.tab("s1")
	time.Sleep(50 * time.Millisecond)
	if tab.isClosed() {
		t.Fatal("stopped the browser of a live session")
	}
	s.Ended = true
	r.d.Post(daemon.SessionChanged{Session: s})
	waitUntil(t, "the browser of the ended session stops", tab.isClosed)
}

func TestCookieImportIsKeptAndGoesToRunningAndNewHeadlessBrowsers(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	var got rpc.BrowserCookiesResult
	err := r.c.Call(context.Background(), rpc.MethodBrowserCookiesImport, rpc.BrowserCookiesParams{Cookies: []domain.BrowserCookie{
		{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/"},
	}}, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Imported != 1 || got.Total != 1 {
		t.Fatalf("result %+v", got)
	}
	if names := r.b.tab("s1").cookieNames(); !reflect.DeepEqual(names, []string{"sid=abc"}) {
		t.Fatalf("running browser cookies %q", names)
	}
	info, err := os.Stat(r.cookies)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	err = r.c.Call(context.Background(), rpc.MethodBrowserCookiesImport, rpc.BrowserCookiesParams{Cookies: []domain.BrowserCookie{
		{Name: "sid", Value: "new", Domain: ".example.com", Path: "/"},
		{Name: "x", Value: "1", Domain: "b.com", Path: "/"},
	}}, &got)
	if err != nil || got.Total != 2 {
		t.Fatalf("second import %+v: %v", got, err)
	}
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s2", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if names := r.b.tab("s2").cookieNames(); !reflect.DeepEqual(names, []string{"sid=new", "x=1"}) {
		t.Fatalf("new browser cookies %q", names)
	}
}

func TestCookieImportWithNoCookiesIsABadRequest(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	err := r.c.Call(context.Background(), rpc.MethodBrowserCookiesImport, rpc.BrowserCookiesParams{}, nil)
	if errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("got %v", err)
	}
}

const headlessAutomations = `
[browser]
fallback = "%s"

[[automation]]
name = "nightly"
agent = "claude"
prompt = "Check the site."
browser = "%s"
`

func startHeadlessAutomation(t *testing.T, fallback, browser string) (automationRig, *fakeHeadless) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	text := strings.Replace(strings.Replace(headlessAutomations, "%s", fallback, 1), "%s", browser, 1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &fakeHeadless{}
	r := automationRig{sessionRig: sessionRig{host: &fakeHost{}, wts: &fakeWorktrees{}}, store: &memStore{}, runner: &fakeRunner{}}
	r.store.snap.Workspaces = append(r.store.snap.Workspaces, orchWS)
	r.d, r.path = start(t, r.store,
		daemon.WithHarnesses(r.host, claude.Adapter{}),
		daemon.WithSessions(r.wts, nil, "/h/worktrees"),
		daemon.WithFirstPromptGrace(time.Hour),
		daemon.WithConfig(path),
		daemon.WithAutomationPrecheck(r.runner),
		daemon.WithHeadlessBrowser(b, filepath.Join(t.TempDir(), "cookies.json"), time.Hour),
	)
	r.c = dial(t, r.path)
	return r, b
}

func TestAnAutomationRunWithoutTheMacAppUsesTheHeadlessBrowser(t *testing.T) {
	r, b := startHeadlessAutomation(t, "headless", "any")
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "nightly", WorkItem: "x", Repo: "/src/shop"})
	if got.Run.Outcome != domain.AutomationSucceeded {
		t.Fatalf("run %+v", got.Run)
	}
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: got.Session.ID, Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if b.tab(got.Session.ID) == nil {
		t.Fatal("no headless browser for the run")
	}
}

func TestAnAutomationWithBrowserHeadlessUsesItWithTheFallbackOff(t *testing.T) {
	r, b := startHeadlessAutomation(t, "off", "headless")
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "nightly", WorkItem: "x", Repo: "/src/shop"})
	attachBrowser(t, r.path)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: got.Session.ID, Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if b.tab(got.Session.ID) == nil {
		t.Fatal("browser = headless did not use the headless browser")
	}
}

func TestAnAutomationWithBrowserMacNeverFallsBack(t *testing.T) {
	r, b := startHeadlessAutomation(t, "headless", "mac")
	got := runAutomation(t, r, rpc.AutomationRunParams{Name: "nightly", WorkItem: "x", Repo: "/src/shop"})
	_, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: got.Session.ID, Command: "snapshot"})
	if errCode(err) != rpc.CodeUnavailable || rpcMessage(err) != rpc.NoBrowser {
		t.Fatalf("got %v", err)
	}
	if b.openCount() != 0 {
		t.Fatal("browser = mac fell back to headless")
	}
}

func TestAHeadlessBrowserThatRefusesImportedCookiesStopsAndTheNextOneGetsThem(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	first := r.b.tab("s1")
	first.mu.Lock()
	first.refuse = true
	first.mu.Unlock()
	err := r.c.Call(context.Background(), rpc.MethodBrowserCookiesImport, rpc.BrowserCookiesParams{Cookies: []domain.BrowserCookie{
		{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !first.isClosed() {
		t.Fatal("kept a browser without the imported cookies")
	}
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if names := r.b.tab("s1").cookieNames(); !reflect.DeepEqual(names, []string{"sid=abc"}) {
		t.Fatalf("new browser cookies %q", names)
	}
}

func headlessView(c *rpc.Client, session string) (rpc.BrowserHeadlessView, error) {
	var out rpc.BrowserHeadlessView
	err := c.Call(context.Background(), rpc.MethodBrowserHeadlessView, rpc.BrowserHeadlessViewParams{Session: session}, &out)
	return out, err
}

func TestTheHeadlessViewOfASessionWithNoHeadlessTabIsNotLiveAndStartsNoBrowser(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	got, err := headlessView(r.c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Live || got.PNG != "" {
		t.Fatalf("view %+v", got)
	}
	if r.b.openCount() != 0 {
		t.Fatal("the view started a headless browser")
	}
}

func TestTheHeadlessViewShowsAScreenshotOfTheRunningTab(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "goto", Args: map[string]any{"url": "http://devbox:3000/"}}); err != nil {
		t.Fatal(err)
	}
	got, err := headlessView(r.c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.BrowserHeadlessView{Live: true, PNG: "iVBORw0KGgo=", Width: 1280, Height: 800}
	if got != want {
		t.Fatalf("view %+v, want %+v", got, want)
	}
	if r.b.openCount() != 1 {
		t.Fatalf("opened %d browsers", r.b.openCount())
	}
}

func TestTheHeadlessViewDoesNotKeepAnIdleTabAlive(t *testing.T) {
	r := startHeadless(t, headlessOn+"idle = \"80ms\"\n", 10*time.Millisecond)
	r.d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s1", Harness: domain.HarnessClaude, State: domain.StateRunning}})
	if _, err := browserCommand(r.c, rpc.BrowserCommandParams{Session: "s1", Command: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	tab := r.b.tab("s1")
	stop := time.After(2 * time.Second)
	for !tab.isClosed() {
		select {
		case <-stop:
			t.Fatal("the view kept the idle tab alive")
		default:
		}
		if _, err := headlessView(r.c, "s1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTheHeadlessViewNeedsASession(t *testing.T) {
	r := startHeadless(t, headlessOn, time.Hour)
	if _, err := headlessView(r.c, ""); errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("got %v", err)
	}
}
