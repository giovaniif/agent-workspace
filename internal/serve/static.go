package serve

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const (
	builtShell       = "app.html"
	placeholderShell = "index.html"
	assetsDir        = "assets/"
	cacheForever     = "public, max-age=31536000, immutable"
	cacheRevalidate  = "no-cache"
)

func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func Static(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "api" || strings.HasPrefix(name, "api/") {
			http.NotFound(w, r)
			return
		}
		if name == "" || name == builtShell || name == placeholderShell || isClientRoute(fsys, name) {
			serveShell(w, r, fsys)
			return
		}
		if !exists(fsys, name) {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, assetsDir) {
			w.Header().Set("Cache-Control", cacheForever)
		} else {
			w.Header().Set("Cache-Control", cacheRevalidate)
		}
		files.ServeHTTP(w, r)
	})
}

func isClientRoute(fsys fs.FS, name string) bool {
	return path.Ext(name) == "" && !exists(fsys, name)
}

func exists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

func serveShell(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	page, err := fs.ReadFile(fsys, builtShell)
	if errors.Is(err, fs.ErrNotExist) {
		page, err = fs.ReadFile(fsys, placeholderShell)
	}
	if err != nil {
		http.Error(w, "the web app is missing; run make web", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheRevalidate)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}
