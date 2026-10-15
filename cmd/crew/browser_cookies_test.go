package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/chromium"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type noHeadless struct{}

func (noHeadless) Open(context.Context, string) (app.HeadlessTab, error) {
	return nil, errors.New("no chromium in this test")
}

func cookieDaemon(t *testing.T) (string, string) {
	t.Helper()
	home := shortHome(t)
	t.Setenv("CREW_HOME", home)
	t.Setenv("CREW_SESSION", "")
	t.Setenv("TMUX_PANE", "")
	jar := filepath.Join(t.TempDir(), "cookies.json")
	d, err := daemon.New(nopStore{}, 1, daemon.WithHeadlessBrowser(noHeadless{}, jar, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return home, jar
}

func TestBrowserCookiesImportSendsTheFileToTheServerProfile(t *testing.T) {
	_, jar := cookieDaemon(t)
	file := filepath.Join(t.TempDir(), "cookies.txt")
	text := "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t0\tsid\tabc\nexample.com\tFALSE\t/\tFALSE\t0\tlang\ten\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"browser", "cookies", "import", file}, &out, &errOut); code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "imported 2 cookies") || strings.Contains(out.String(), "abc") {
		t.Fatalf("stdout %q", out.String())
	}
	data, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	var kept []domain.BrowserCookie
	if err := json.Unmarshal(data, &kept); err != nil || len(kept) != 2 {
		t.Fatalf("kept %s: %v", data, err)
	}
}

func TestBrowserCookiesImportOfABadFileFailsBeforeTheDaemon(t *testing.T) {
	t.Setenv("CREW_HOME", shortHome(t))
	file := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(file, []byte(`{"hello":"world"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"browser", "cookies", "import", file}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "cookies") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if code := run([]string{"browser", "cookies", "import"}, &out, &errOut); code != 2 {
		t.Fatalf("no file: code %d", code)
	}
}

func TestSetupClaudeSaysWhetherChromiumIsInstalled(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CREW_CHROMIUM", "/opt/chrome/chrome")
	var out, errOut bytes.Buffer
	if code := run([]string{"setup", "claude"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "headless browser: /opt/chrome/chrome") {
		t.Fatalf("stdout %q", out.String())
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CREW_CHROMIUM", "")
	t.Setenv("PATH", t.TempDir())
	if bin := chromium.FromEnv().Binary; bin != "" {
		t.Skipf("Chromium at a fixed path: %s", bin)
	}
	out.Reset()
	if code := run([]string{"setup", "claude"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no headless browser") || !strings.Contains(out.String(), "CREW_CHROMIUM") {
		t.Fatalf("stdout %q", out.String())
	}
}
