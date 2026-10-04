package serve_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/giovaniif/agent-workspace/internal/serve"
)

const (
	appHTML         = "<!doctype html><title>agentws</title><div id=\"root\"></div>"
	placeholderHTML = "<!doctype html><p>Run make web</p>"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte(placeholderHTML)},
		"app.html":              {Data: []byte(appHTML)},
		"sw.js":                 {Data: []byte("self.addEventListener(\"fetch\", () => {});")},
		"manifest.webmanifest":  {Data: []byte("{}")},
		"assets/index-abc1.js":  {Data: []byte("export {};")},
		"assets/index-abc1.css": {Data: []byte("body{}")},
	}
}

func get(t *testing.T, fsys fs.FS, method, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	serve.Static(fsys).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStaticServesTheBuiltAppAtRoot(t *testing.T) {
	res := get(t, built(), http.MethodGet, "/")
	if res.StatusCode != http.StatusOK || body(t, res) != appHTML {
		t.Fatalf("GET / = %d, want 200 with the app", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache so a new build is seen", got)
	}
}

func TestStaticServesThePlaceholderWithoutABuild(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte(placeholderHTML)}}
	if res := get(t, fsys, http.MethodGet, "/"); res.StatusCode != http.StatusOK || body(t, res) != placeholderHTML {
		t.Fatalf("GET / = %d, want 200 with the placeholder", res.StatusCode)
	}
}

func TestStaticServesTheAppForClientRoutes(t *testing.T) {
	for _, path := range []string{"/sessions", "/sessions/abc"} {
		if res := get(t, built(), http.MethodGet, path); res.StatusCode != http.StatusOK || body(t, res) != appHTML {
			t.Errorf("GET %s = %d, want 200 with the app", path, res.StatusCode)
		}
	}
}

func TestStaticCachesHashedAssetsForever(t *testing.T) {
	res := get(t, built(), http.MethodGet, "/assets/index-abc1.js")
	if res.StatusCode != http.StatusOK || body(t, res) != "export {};" {
		t.Fatalf("GET asset = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
	if got := res.Header.Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", got)
	}
}

func TestStaticRevalidatesTheServiceWorkerAndManifest(t *testing.T) {
	for _, path := range []string{"/sw.js", "/manifest.webmanifest"} {
		res := get(t, built(), http.MethodGet, path)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, res.StatusCode)
		}
		if got := res.Header.Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache", path, got)
		}
	}
}

func TestStaticAnswersNotFoundForMissingFilesAndTheAPI(t *testing.T) {
	for _, path := range []string{"/missing.png", "/assets/gone-1.js", "/api/v1/hello", "/api/v1/sessions"} {
		if res := get(t, built(), http.MethodGet, path); res.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.StatusCode)
		}
	}
}

func TestStaticRefusesWrites(t *testing.T) {
	if res := get(t, built(), http.MethodPost, "/"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST / = %d, want 405", res.StatusCode)
	}
	if res := get(t, built(), http.MethodHead, "/"); res.StatusCode != http.StatusOK {
		t.Fatalf("HEAD / = %d, want 200", res.StatusCode)
	}
}

func TestStaticEmbedsThePlaceholderThatAsksForMakeWeb(t *testing.T) {
	b, err := fs.ReadFile(serve.Dist(), "index.html")
	if err != nil || !strings.Contains(string(b), "make web") {
		t.Fatalf("embedded index.html = %q, %v; want a placeholder that says to run make web", b, err)
	}
}

func TestStaticServesTheEmbeddedBuildAfterMakeWeb(t *testing.T) {
	if os.Getenv("AGENTWS_WEB_BUILT") == "" {
		t.Skip("set AGENTWS_WEB_BUILT=1 after make web")
	}
	res := get(t, serve.Dist(), http.MethodGet, "/")
	page := body(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(page, "id=\"root\"") || !strings.Contains(page, "manifest.webmanifest") {
		t.Fatalf("GET / = %d %q, want the built app", res.StatusCode, page)
	}
	if res := get(t, serve.Dist(), http.MethodGet, "/sw.js"); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /sw.js = %d, want the built service worker", res.StatusCode)
	}
}
