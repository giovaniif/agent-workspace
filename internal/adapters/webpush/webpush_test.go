package webpush_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/webpush"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.PushProvider = (*webpush.Sender)(nil)

type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return browser{key: key, auth: auth}
}

func (b browser) subscription(endpoint string) domain.PushSubscription {
	return domain.PushSubscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(b.key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(b.auth),
	}
}

func (b browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body of %d bytes", len(body))
	}
	salt := body[:16]
	idLen := int(body[20])
	serverPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idLen])
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := body[21+idLen:]
	shared, err := b.key.ECDH(serverPub)
	if err != nil {
		t.Fatal(err)
	}
	info := "WebPush: info\x00" + string(b.key.PublicKey().Bytes()) + string(serverPub.Bytes())
	ikm, err := hkdf.Key(sha256.New, shared, b.auth, info, 32)
	if err != nil {
		t.Fatal(err)
	}
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		t.Fatalf("no record delimiter in %q", plain)
	}
	return plain[:len(plain)-1]
}

type pushService struct {
	*httptest.Server
	mu       sync.Mutex
	status   int
	requests []received
}

type received struct {
	header http.Header
	body   []byte
	path   string
}

func newPushService(t *testing.T) *pushService {
	t.Helper()
	p := &pushService{status: http.StatusCreated}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.requests = append(p.requests, received{header: r.Header.Clone(), body: body, path: r.URL.Path})
		status := p.status
		p.mu.Unlock()
		if status >= 300 && status < 400 {
			w.Header().Set("Location", "/elsewhere")
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *pushService) setStatus(code int) {
	p.mu.Lock()
	p.status = code
	p.mu.Unlock()
}

func (p *pushService) last(t *testing.T) received {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		t.Fatal("the push service got nothing")
	}
	return p.requests[len(p.requests)-1]
}

func verifyVAPID(t *testing.T, header, publicKey, audience string) {
	t.Helper()
	rest, ok := strings.CutPrefix(header, "vapid t=")
	if !ok {
		t.Fatalf("Authorization %q", header)
	}
	jwt, k, ok := strings.Cut(rest, ", k=")
	if !ok || k != publicKey {
		t.Fatalf("Authorization key %q, want %q", k, publicKey)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	claimsJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != audience || (!strings.HasPrefix(claims.Sub, "https://") && !strings.HasPrefix(claims.Sub, "mailto:")) {
		t.Fatalf("claims %+v, want aud %s", claims, audience)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(publicKey)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, sum[:], r, s) {
		t.Fatal("the VAPID token is not signed by the public key")
	}
}

func TestWebPushCreatesTheVAPIDKeysOnFirstUseAsAFileOnlyTheUserReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid")
	s := webpush.New(path, nil)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("keys exist before first use: %v", err)
	}
	key, err := s.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		t.Fatalf("public key %q", key)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	again, err := webpush.New(path, nil).PublicKey()
	if err != nil || again != key {
		t.Fatalf("a second sender read %q, %v; want %q", again, err, key)
	}
}

func TestWebPushRefusesAnUnreadableKeyFileAndLeavesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := webpush.New(path, nil).PublicKey(); err == nil {
		t.Fatal("a broken key file was accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != "not json" {
		t.Fatalf("the key file was replaced with %q", b)
	}
}

func TestWebPushSendsTheMessageEncryptedForTheBrowserAndSigned(t *testing.T) {
	svc := newPushService(t)
	s := webpush.New(filepath.Join(t.TempDir(), "vapid"), svc.Client())
	b := newBrowser(t)
	msg := domain.PushMessage{Title: "api · api@fix-login", Body: "needs permission: Bash: rm -rf dist", URL: "/#/sessions/s1", Tag: "s1"}
	if err := s.Send(context.Background(), b.subscription(svc.URL+"/push/abc"), msg); err != nil {
		t.Fatal(err)
	}
	got := svc.last(t)
	if got.path != "/push/abc" {
		t.Fatalf("path %q", got.path)
	}
	if ce := got.header.Get("Content-Encoding"); ce != "aes128gcm" {
		t.Fatalf("Content-Encoding %q", ce)
	}
	if ttl := got.header.Get("TTL"); ttl == "" || ttl == "0" {
		t.Fatalf("TTL %q", ttl)
	}
	if u := got.header.Get("Urgency"); u != "high" {
		t.Fatalf("Urgency %q", u)
	}
	key, _ := s.PublicKey()
	verifyVAPID(t, got.header.Get("Authorization"), key, svc.URL)
	var decoded domain.PushMessage
	if err := json.Unmarshal(b.decrypt(t, got.body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != msg {
		t.Fatalf("payload %+v", decoded)
	}
	if rs := binary.BigEndian.Uint32(got.body[16:20]); rs < 18 {
		t.Fatalf("record size %d", rs)
	}
}

func TestWebPushTreatsNotFoundAndGoneAsAGoneSubscription(t *testing.T) {
	svc := newPushService(t)
	s := webpush.New(filepath.Join(t.TempDir(), "vapid"), svc.Client())
	sub := newBrowser(t).subscription(svc.URL + "/push/abc")
	msg := domain.PushMessage{Title: "t", Body: "b"}
	for _, code := range []int{http.StatusNotFound, http.StatusGone} {
		svc.setStatus(code)
		if err := s.Send(context.Background(), sub, msg); !errors.Is(err, app.ErrPushGone) {
			t.Fatalf("status %d: %v", code, err)
		}
	}
	for _, code := range []int{http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusForbidden} {
		svc.setStatus(code)
		err := s.Send(context.Background(), sub, msg)
		if err == nil || errors.Is(err, app.ErrPushGone) {
			t.Fatalf("status %d: %v", code, err)
		}
	}
	for _, code := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		svc.setStatus(code)
		if err := s.Send(context.Background(), sub, msg); err != nil {
			t.Fatalf("status %d: %v", code, err)
		}
	}
}

func (p *pushService) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func TestWebPushFollowsNoRedirect(t *testing.T) {
	svc := newPushService(t)
	svc.setStatus(http.StatusTemporaryRedirect)
	s := webpush.New(filepath.Join(t.TempDir(), "vapid"), svc.Client())
	err := s.Send(context.Background(), newBrowser(t).subscription(svc.URL+"/push/abc"), domain.PushMessage{Title: "t", Body: "b"})
	if err == nil || errors.Is(err, app.ErrPushGone) {
		t.Fatalf("a redirect was taken as %v", err)
	}
	if n := svc.count(); n != 1 {
		t.Fatalf("the push service got %d requests", n)
	}
}

func TestWebPushReachesNoLoopbackOrPrivateAddressByDefault(t *testing.T) {
	var hits int
	var mu sync.Mutex
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(internal.Close)
	s := webpush.New(filepath.Join(t.TempDir(), "vapid"), nil)
	if err := s.Send(context.Background(), newBrowser(t).subscription(internal.URL+"/admin"), domain.PushMessage{Title: "t", Body: "b"}); err == nil {
		t.Fatal("a push to a loopback address was sent")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("the loopback server got %d requests", hits)
	}
}
