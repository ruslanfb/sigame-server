package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// reservedPrefixes are server routes that must never fall back to the SPA
// index (a wrong API path must be a 404, not a 200 with HTML).
var reservedPrefixes = []string{"/api/", "/media/", "/ws", "/docs", "/openapi", "/schemas/"}

// spaHandler serves a built single-page application from fsys: files are
// served as-is (hashed assets under /assets/ with immutable caching), every
// other GET path renders index.html so client-side routes work on reload.
func spaHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		for _, pre := range reservedPrefixes {
			if strings.HasPrefix(p, pre) || p == strings.TrimSuffix(pre, "/") {
				http.NotFound(w, r)
				return
			}
		}
		name := strings.TrimPrefix(p, "/")
		if name != "" {
			if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				r.URL.Path = p
				files.ServeHTTP(w, r)
				return
			}
			// A path that looks like a file (has an extension) but does not
			// exist is a 404; extension-less paths are client routes.
			if ext := path.Ext(name); ext != "" && ext != "." {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, r, fsys)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "web ui is not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}

// hasIndex reports whether fsys contains a built SPA.
func hasIndex(fsys fs.FS) bool {
	if fsys == nil {
		return false
	}
	st, err := fs.Stat(fsys, "index.html")
	return err == nil && !st.IsDir()
}
