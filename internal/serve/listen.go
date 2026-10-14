package serve

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultAddr       = "127.0.0.1:7420"
	selfSignedFor     = 5 * 365 * 24 * time.Hour
	selfSignedRenew   = 30 * 24 * time.Hour
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 10 * time.Second
)

type Options struct {
	Addr       string
	Cert       string
	Key        string
	SelfSigned bool
	CertDir    string
}

func (o Options) TLS() bool { return o.Cert != "" || o.SelfSigned }

func (o Options) Check() error {
	if (o.Cert == "") != (o.Key == "") {
		return errors.New("--cert and --key go together")
	}
	if o.Cert != "" && o.SelfSigned {
		return errors.New("--self-signed replaces --cert and --key")
	}
	host, _, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return fmt.Errorf("--addr %q: %w", o.Addr, err)
	}
	if !o.TLS() && !loopback(host) {
		return fmt.Errorf("--addr %q: plain HTTP is only served on loopback, so a device token never crosses a network in clear; pass --cert and --key, or --self-signed for a proxy that cannot reach loopback", o.Addr)
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func Listen(ctx context.Context, o Options, h http.Handler, ready func(url string)) error {
	if err := o.Check(); err != nil {
		return err
	}
	cert, key := o.Cert, o.Key
	if o.SelfSigned {
		host, _, _ := net.SplitHostPort(o.Addr)
		var err error
		if cert, key, err = SelfSigned(o.CertDir, host); err != nil {
			return err
		}
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	scheme := "http"
	if o.TLS() {
		scheme = "https"
	}
	if ready != nil {
		ready(scheme + "://" + ln.Addr().String())
	}
	if o.TLS() {
		err = srv.ServeTLS(ln, cert, key)
	} else {
		err = srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}

func SelfSigned(dir, host string) (certFile, keyFile string, err error) {
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if fresh(certFile, keyFile, host) {
		return certFile, keyFile, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "crew serve"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, name := range certNames(host) {
		if ip := net.ParseIP(name); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, name)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func certNames(host string) []string {
	names := []string{"localhost", "127.0.0.1", "::1", "host.docker.internal"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		names = append(names, hostname)
	}
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsUnspecified()) {
		names = append(names, host)
	}
	return names
}

func fresh(certFile, keyFile, host string) bool {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Until(cert.NotAfter) < selfSignedRenew {
		return false
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		return true
	}
	return cert.VerifyHostname(host) == nil
}
