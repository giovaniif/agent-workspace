package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
)

func TestServeRefusesPlainHTTPOffLoopbackWhateverTheFlags(t *testing.T) {
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	for _, args := range [][]string{
		{"--addr", "0.0.0.0:7420"},
		{"--addr", "0.0.0.0:7420", "--url", "https://box.example.ts.net"},
		{"--addr", "0.0.0.0:7420", "--cert", "c.pem"},
		{"--addr", "0.0.0.0:7420", "--key", "k.pem"},
		{"--addr", ":7420"},
		{"--addr", "192.168.15.8:7420"},
		{"--url", "https://box.example.ts.net", "--addr", "box.example.ts.net:7420"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(append([]string{"serve"}, args...), &stdout, &stderr)
		if code == 0 || stdout.Len() != 0 {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if _, err := os.Stat(rpc.SocketPath(home)); err == nil {
		t.Fatal("a refused serve started the daemon")
	}
}

func TestServeSaysWhyPlainHTTPIsRefused(t *testing.T) {
	t.Setenv("AGENTWS_HOME", shortHome(t))
	var stderr bytes.Buffer
	code := run([]string{"serve", "--addr", "0.0.0.0:7420"}, io.Discard, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "loopback") || !strings.Contains(stderr.String(), "--cert") || !strings.Contains(stderr.String(), "--self-signed") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestServeUsage(t *testing.T) {
	t.Setenv("AGENTWS_HOME", shortHome(t))
	for _, args := range [][]string{{"serve", "extra"}, {"serve", "--nope"}} {
		var stderr bytes.Buffer
		if code := run(args, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: agentws serve") {
			t.Fatalf("%v: exit %d, stderr %q", args, code, stderr.String())
		}
	}
}

func TestServeFlagsDefaultToLoopbackAndTheConfiguredURL(t *testing.T) {
	home := shortHome(t)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[serve]\nurl = \"https://box.example.ts.net\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := parseServe(nil, home, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := serve.Options{Addr: "127.0.0.1:7420", CertDir: filepath.Join(home, "serve")}
	if f.opts != want || f.url != "https://box.example.ts.net" {
		t.Fatalf("flags %+v", f)
	}
	f, err = parseServe([]string{"--url", "https://other.example", "--addr", "0.0.0.0:7420", "--self-signed"}, home, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if f.url != "https://other.example" || !f.opts.SelfSigned || f.opts.Addr != "0.0.0.0:7420" {
		t.Fatalf("flags %+v", f)
	}
	f, err = parseServe([]string{"--addr", "0.0.0.0:7420", "--cert", "c.pem", "--key", "k.pem"}, home, io.Discard)
	if err != nil || f.opts.Cert != "c.pem" || f.opts.Key != "k.pem" {
		t.Fatalf("flags %+v, err %v", f, err)
	}
}

func TestServeServesTheEmbeddedWebAppAtRoot(t *testing.T) {
	srv, err := serve.New(serve.Config{Build: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	serveHandler(srv).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatalf("GET / = %d %q, want the embedded web app", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	serveHandler(srv).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/hello", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"api\":\"v1\"") {
		t.Fatalf("GET /api/v1/hello = %d %q, want the API", rec.Code, rec.Body.String())
	}
}
