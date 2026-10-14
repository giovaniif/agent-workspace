package serve_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/serve"
)

func TestServeOptionsRefusePlainHTTPOffLoopback(t *testing.T) {
	cases := []struct {
		opts serve.Options
		ok   bool
	}{
		{serve.Options{Addr: serve.DefaultAddr}, true},
		{serve.Options{Addr: "localhost:7420"}, true},
		{serve.Options{Addr: "[::1]:7420"}, true},
		{serve.Options{Addr: "127.0.0.2:7420"}, true},
		{serve.Options{Addr: "0.0.0.0:7420"}, false},
		{serve.Options{Addr: ":7420"}, false},
		{serve.Options{Addr: "[::]:7420"}, false},
		{serve.Options{Addr: "192.168.15.8:7420"}, false},
		{serve.Options{Addr: "100.99.215.28:7420"}, false},
		{serve.Options{Addr: "host.example.ts.net:7420"}, false},
		{serve.Options{Addr: "0.0.0.0:7420", Cert: "c.pem"}, false},
		{serve.Options{Addr: "0.0.0.0:7420", Key: "k.pem"}, false},
		{serve.Options{Addr: "0.0.0.0:7420", Cert: "c.pem", Key: "k.pem"}, true},
		{serve.Options{Addr: "0.0.0.0:7420", SelfSigned: true}, true},
		{serve.Options{Addr: "0.0.0.0:7420", Cert: "c.pem", Key: "k.pem", SelfSigned: true}, false},
		{serve.Options{Addr: "7420"}, false},
	}
	for _, tc := range cases {
		err := tc.opts.Check()
		if (err == nil) != tc.ok {
			t.Errorf("%+v: err %v, want ok=%v", tc.opts, err, tc.ok)
		}
	}
}

func TestServeListenRefusesPlainHTTPOffLoopbackBeforeListening(t *testing.T) {
	err := serve.Listen(context.Background(), serve.Options{Addr: "0.0.0.0:0"}, http.NotFoundHandler(), func(string) {
		t.Fatal("it listened")
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err %v", err)
	}
}

func TestServeSelfSignedCertificateCoversLoopbackAndIsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "serve")
	certFile, keyFile, err := serve.SelfSigned(dir, "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(keyFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v %v", info, err)
	}
	first, _ := os.ReadFile(certFile)
	block, _ := pem.Decode(first)
	if block == nil {
		t.Fatal("no PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"localhost", "127.0.0.1", "10.1.2.3", "host.docker.internal"} {
		if err := cert.VerifyHostname(host); err != nil {
			t.Errorf("certificate does not cover %s: %v", host, err)
		}
	}
	if time.Until(cert.NotAfter) < 365*24*time.Hour {
		t.Errorf("certificate expires %v", cert.NotAfter)
	}
	if _, _, err := serve.SelfSigned(dir, "10.1.2.3"); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(certFile)
	if !bytes.Equal(first, again) {
		t.Fatal("a second start replaced a certificate the proxy already trusts")
	}
	if _, _, err := serve.SelfSigned(dir, "10.9.9.9"); err != nil {
		t.Fatal(err)
	}
	renewed, _ := os.ReadFile(certFile)
	if bytes.Equal(first, renewed) {
		t.Fatal("a certificate that does not cover the new address was kept")
	}
}

func TestServeListenServesTLSWithTheSelfSignedCertificate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "serve")
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve.Listen(ctx, serve.Options{Addr: "127.0.0.1:0", SelfSigned: true, CertDir: dir},
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
			func(u string) { urls <- u })
	}()
	var url string
	select {
	case url = <-urls:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("not listening")
	}
	if !strings.HasPrefix(url, "https://127.0.0.1:") {
		t.Fatalf("url %s", url)
	}
	pemBytes, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body %q", body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Listen did not return after its context ended")
	}
}
