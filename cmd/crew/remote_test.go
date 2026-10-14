package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func startRemoteDaemon(t *testing.T) (string, *rpc.Client) {
	t.Helper()
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	d, err := daemon.New(nopStore{}, 1)
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
	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return home, c
}

func TestRemotePairWithoutAURLSaysHowToSetOne(t *testing.T) {
	t.Setenv("AGENTWS_HOME", shortHome(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"remote", "pair"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "--url") || !strings.Contains(stderr.String(), "[serve]") || !strings.Contains(stderr.String(), "config.toml") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

var pairURL = regexp.MustCompile(`https://box\.example\.ts\.net/#pair=([A-Z0-9]{8})`)

func TestRemotePairPrintsTheURLAQRCodeAndTheExpiry(t *testing.T) {
	home, c := startRemoteDaemon(t)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[serve]\nurl = \"https://box.example.ts.net\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"remote", "pair", "--name", "phone"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	out := stdout.String()
	m := pairURL.FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "expires") || !strings.ContainsAny(out, "█▀▄") {
		t.Fatalf("output:\n%s", out)
	}
	var got rpc.PairRedeemed
	if err := c.Call(context.Background(), rpc.MethodPairRedeem, rpc.PairRedeemParams{Code: m[1], Addr: "100.64.0.2"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Device.Name != "phone" {
		t.Fatalf("device %+v", got.Device)
	}
}

func TestRemotePairURLFlagWinsOverTheConfig(t *testing.T) {
	home, _ := startRemoteDaemon(t)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[serve]\nurl = \"other.example\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"remote", "pair", "--url", "box.example.ts.net"}, &stdout, &stderr); code != 0 || !pairURL.MatchString(stdout.String()) {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRemotePairRefusesAPlainHTTPURL(t *testing.T) {
	startRemoteDaemon(t)
	var stderr bytes.Buffer
	if code := run([]string{"remote", "pair", "--url", "http://box.example.ts.net"}, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "https") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestRemoteDevicesListsThemAndRevokeRemovesOne(t *testing.T) {
	_, c := startRemoteDaemon(t)
	ctx := context.Background()
	var code rpc.PairCode
	if err := c.Call(ctx, rpc.MethodPairCode, rpc.PairCodeParams{Name: "phone"}, &code); err != nil {
		t.Fatal(err)
	}
	var got rpc.PairRedeemed
	if err := c.Call(ctx, rpc.MethodPairRedeem, rpc.PairRedeemParams{Code: code.Code, Addr: "100.64.0.2"}, &got); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run([]string{"remote", "devices"}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), got.Device.ID) || !strings.Contains(stdout.String(), "phone") {
		t.Fatalf("exit %d, stdout %q, stderr %q", exit, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), got.Token) {
		t.Fatal("devices printed a token")
	}
	stdout.Reset()
	if exit := run([]string{"remote", "revoke", got.Device.ID}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "revoked "+got.Device.ID) {
		t.Fatalf("exit %d, stdout %q, stderr %q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if exit := run([]string{"remote", "devices"}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "no paired devices") {
		t.Fatalf("exit %d, stdout %q", exit, stdout.String())
	}
	stderr.Reset()
	if exit := run([]string{"remote", "revoke", got.Device.ID}, &stdout, &stderr); exit != 1 || !strings.Contains(stderr.String(), got.Device.ID) {
		t.Fatalf("revoke again: exit %d, stderr %q", exit, stderr.String())
	}
}

func TestRemoteUsage(t *testing.T) {
	for _, args := range [][]string{{"remote"}, {"remote", "nope"}, {"remote", "revoke"}, {"remote", "devices", "x"}} {
		var stderr bytes.Buffer
		if code := run(args, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: agentws remote") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr.String())
		}
	}
}

func TestRemoteQRUsesTwoModuleRowsPerLineWithLightModulesDrawn(t *testing.T) {
	diagonal := func(x, y int) bool { return x == y }
	got := halfBlocks(2, diagonal, 1)
	if want := "█▀██\n██▄█\n"; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	odd := halfBlocks(1, func(int, int) bool { return true }, 1)
	if want := "█▀█\n▀▀▀\n"; odd != want {
		t.Fatalf("odd height got:\n%s\nwant:\n%s", odd, want)
	}
}
