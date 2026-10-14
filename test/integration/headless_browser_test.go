//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/chromium"
	"github.com/giovaniif/crew/internal/adapters/sqlite"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const headlessPage = `<!doctype html><html><head><title>Sign in</title></head><body>
<h1>Sign in</h1>
<label for="name">Name</label><input id="name">
<button onclick="document.getElementById('out').textContent = 'hello ' + document.getElementById('name').value">Greet</button>
<p id="out"></p>
</body></html>`

func startHeadlessDaemon(t *testing.T) *rpc.Client {
	t.Helper()
	bin := chromium.FromEnv().Binary
	if bin == "" {
		t.Skip("no Chromium: " + chromium.InstallHint())
	}
	home, err := os.MkdirTemp("", "crewhb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, []byte("[browser]\nfallback = \"headless\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := chromium.Browsers{Binary: bin, Flags: []string{"--no-sandbox"}}
	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := daemon.New(store, os.Getpid(), daemon.WithConfig(cfg), daemon.WithHeadlessBrowser(b, filepath.Join(home, "cookies.json"), time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Serve(ctx, ln)
		_ = store.Close()
		close(done)
	}()
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		<-done
	})
	return c
}

func browse(t *testing.T, c *rpc.Client, command string, args map[string]any) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out json.RawMessage
	p := rpc.BrowserCommandParams{Session: "s1", Command: command, Args: args, TimeoutMS: 45000}
	if err := c.Call(ctx, rpc.MethodBrowserCommand, p, &out); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return out
}

func refOf(t *testing.T, snapshot, role, name string) string {
	t.Helper()
	m := regexp.MustCompile(`- ` + role + ` "` + regexp.QuoteMeta(name) + `" \[(@e[0-9]+)\]`).FindStringSubmatch(snapshot)
	if m == nil {
		t.Fatalf("no %s %q in\n%s", role, name, snapshot)
	}
	return m[1]
}

func TestHeadlessChromiumRunsTheBrowserCommandsWithoutTheMacApp(t *testing.T) {
	c := startHeadlessDaemon(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, headlessPage)
	}))
	defer srv.Close()

	var opened struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(browse(t, c, "goto", map[string]any{"url": srv.URL}), &opened); err != nil || opened.Title != "Sign in" {
		t.Fatalf("goto: %+v %v", opened, err)
	}
	var snap struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(browse(t, c, "snapshot", nil), &snap); err != nil {
		t.Fatal(err)
	}
	browse(t, c, "fill", map[string]any{"ref": refOf(t, snap.Text, "textbox", "Name"), "text": "crew"})
	browse(t, c, "click", map[string]any{"ref": refOf(t, snap.Text, "button", "Greet")})
	browse(t, c, "wait", map[string]any{"text": "hello crew"})

	var shot struct {
		PNG   string `json:"png"`
		Width int    `json:"width"`
	}
	if err := json.Unmarshal(browse(t, c, "screenshot", nil), &shot); err != nil || shot.Width == 0 {
		t.Fatalf("screenshot: %v", err)
	}
	if raw, err := base64.StdEncoding.DecodeString(shot.PNG); err != nil || !strings.HasPrefix(string(raw), "\x89PNG") {
		t.Fatalf("not a PNG: %v", err)
	}

	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	err := c.Call(context.Background(), rpc.MethodBrowserCookiesImport, rpc.BrowserCookiesParams{Cookies: []domain.BrowserCookie{
		{Name: "sid", Value: "secret", Domain: host, Path: "/"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cookies := string(browse(t, c, "cookies", map[string]any{"domain": host}))
	if !strings.Contains(cookies, "sid") || strings.Contains(cookies, "secret") {
		t.Fatalf("cookies %s", cookies)
	}
	var value string
	if err := json.Unmarshal(browse(t, c, "eval", map[string]any{"js": "document.cookie"}), &value); err != nil || value != "sid=secret" {
		t.Fatalf("document.cookie %q: %v", value, err)
	}
}
